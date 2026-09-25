package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/WindowsSov8forUs/glyccat/processor"
	"github.com/WindowsSov8forUs/glyccat/sys"
	"github.com/WindowsSov8forUs/glyccat/version"

	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func main() {
	fastStart := flag.Bool("faststart", false, "fast startup")
	debug := flag.Bool("debug", false, "debug mode")
	configPath := flag.String("config", "config.yml", "配置文件路径")
	initialize := flag.Bool("init", false, "交互式初始化配置，不覆盖已有文件")
	updateConfig := flag.Bool("update-config", false, "备份并显式迁移配置")
	legacyMessages := flag.String("migrate-messages", "", "旧消息数据库的停机备份目录，仅显式指定时迁移")
	migrationTarget := flag.String("migration-target", database.DefaultMessageStorePath, "迁移后的新消息数据库目录")
	migrationAppID := flag.String("migration-app-id", "", "显式确认旧消息库所属的 AppID")
	migrationSelfID := flag.String("migration-self-id", "", "显式确认旧消息库所属的新版 self_id")
	flag.Parse()

	modes := 0
	for _, enabled := range []bool{*initialize, *updateConfig, *legacyMessages != ""} {
		if enabled {
			modes++
		}
	}
	if modes > 1 {
		fmt.Fprintln(os.Stderr, "初始化配置、迁移配置和迁移消息不能同时执行")
		os.Exit(2)
	}
	if *legacyMessages != "" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		result, err := database.MigrateMessages(ctx, *legacyMessages, *migrationTarget, *migrationAppID, *migrationSelfID)
		cancel()
		fmt.Printf("消息迁移结果：读取 %d 条，导入 %d 条，已存在 %d 条，无法迁移 %d 条\n", result.Read, result.Imported, result.Skipped, result.Failed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "消息迁移失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *initialize {
		if err := config.InitializeConfig(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("配置已保存至 %s\n", *configPath)
		return
	}
	if *updateConfig {
		backup, err := config.UpdateConfig(*configPath)
		if backup != "" {
			fmt.Printf("原配置备份: %s\n", backup)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "迁移配置失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if !*fastStart {
		sys.InitBase()
	}

	fmt.Println(version.Logo())
	versionString := log.StringCenter(fmt.Sprintf("GlycCat %s", version.Version), 58)
	log.PrintlnCyan(versionString)
	fmt.Print("\n==========================================================\n\n")

	conf, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("%s load config failed: %v\n", log.FailMark, log.Red(fmt.Sprint(err)))
		os.Exit(1)
		return
	}

	log.SetLogLevel(conf.LogLevel)

	if *debug {
		log.SetLogLevel(log.DEBUG)
	}

	log.GetLogger()

	if conf.FileServer.Enable {
		log.Warn("旧文件服务器已退出普通发送链路，媒体请使用 upload.create；原文件数据保持不变")
	}

	var messageStore *database.MessageStore
	if conf.Database.MessageDatabase.Enable {
		messageStore, err = database.OpenMessageStore(database.DefaultMessageStorePath, conf.Database.MessageDatabase.Limit)
		if err != nil {
			log.Errorf("启动消息数据库失败: %v", err)
			os.Exit(1)
		}
		defer func() {
			if err := messageStore.Close(); err != nil {
				log.Errorf("关闭消息数据库失败: %v", err)
			}
		}()
	} else {
		log.Warn("消息数据库未启用，群聊和私聊历史缓存不可用")
	}

	runtime, err := newRuntime(conf, messageStore)
	if err != nil {
		log.Fatalf("initialize runtime failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 2)
	go func() {
		runErrCh <- runtime.satoriServer.Run(ctx)
	}()

	if runtime.qqWebhookServer != nil {
		go func() {
			err := runtime.qqWebhookServer.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				runErrCh <- err
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		log.Info("received shutdown signal")
	case runErr := <-runErrCh:
		if runErr != nil {
			log.Errorf("runtime failed: %v", runErr)
		}
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if runtime.qqWebhookServer != nil {
		if err := runtime.qqWebhookServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			log.Errorf("shutdown qq webhook server failed: %v", err)
		}
	}
	if err := runtime.satoriServer.Shutdown(shutdownCtx); err != nil {
		log.Errorf("shutdown satori server failed: %v", err)
	}
}

type runtimeBundle struct {
	satoriServer    *server.Server
	qqWebhookServer *http.Server
}

func newRuntime(conf *config.Config, messageStore *database.MessageStore) (*runtimeBundle, error) {
	if err := conf.NormalizeAndValidate(); err != nil {
		return nil, err
	}

	adapterCfg := qq.Config{
		AppID:         conf.Account.AppID,
		Secret:        conf.Account.AppSecret,
		Sandbox:       conf.Account.Sandbox,
		Path:          conf.Account.WebHook.Path,
		Adapter:       "GlycCat",
		UseWebSocket:  conf.Account.WebSocket.Enable,
		WSIntentNames: conf.Account.WebSocket.Intents,
		WSShardCount:  conf.Account.WebSocket.ShardCount,
	}
	if conf.Account.WebSocket.ShardID != nil {
		adapterCfg.WSShardID = *conf.Account.WebSocket.ShardID
	}

	innerAdapter, err := qq.New(adapterCfg)
	if err != nil {
		return nil, err
	}

	satoriVersion := fmt.Sprintf("v%d", conf.Satori.Version)
	serverHeader := fmt.Sprintf("GlycCat/%s", version.Version)
	apiRouter := chi.NewRouter()
	apiRouter.Use(responseHeaderMiddleware(satoriVersion, serverHeader))
	srv, err := server.NewServer(server.Config{
		Host:          conf.Satori.Server.Host,
		Port:          int(conf.Satori.Server.Port),
		Path:          conf.Satori.Path,
		Version:       satoriVersion,
		Token:         conf.Satori.Token,
		ReplaceRouter: apiRouter,
		// 仅约束 Satori 反向推送，不改变 QQ 请求或资源代理的超时。
		HTTPClient: &http.Client{Timeout: time.Duration(conf.Satori.WebHook.Timeout) * time.Second},
	})
	if err != nil {
		return nil, err
	}
	appAdapter, err := processor.NewAdapter(innerAdapter, messageStore, strconv.FormatUint(conf.Account.AppID, 10))
	if err != nil {
		return nil, err
	}
	if applyErr := srv.Apply(appAdapter); applyErr != nil {
		return nil, applyErr
	}

	webhookServer := buildQQWebhookServer(conf, appAdapter, satoriVersion, serverHeader)

	return &runtimeBundle{
		satoriServer:    srv,
		qqWebhookServer: webhookServer,
	}, nil
}

func buildQQWebhookServer(conf *config.Config, registrar server.RootRouteRegistrar, satoriVersion, serverHeader string) *http.Server {
	if !conf.Account.WebHook.Enable {
		return nil
	}

	webhookHost := conf.Account.WebHook.Host
	webhookPort := conf.Account.WebHook.Port
	if isSameListenEndpoint(webhookHost, webhookPort, conf.Satori.Server.Host, conf.Satori.Server.Port) {
		return nil
	}

	router := chi.NewRouter()
	router.Use(responseHeaderMiddleware(satoriVersion, serverHeader))
	registrar.RegisterRootRoutes(router)

	return &http.Server{
		Addr:              net.JoinHostPort(webhookHost, strconv.Itoa(int(webhookPort))),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func isSameListenEndpoint(hostA string, portA uint16, hostB string, portB uint16) bool {
	return portA == portB && strings.EqualFold(hostA, hostB)
}

func responseHeaderMiddleware(satoriVersion, serverHeader string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if serverHeader != "" {
				w.Header().Set("Server", serverHeader)
			}
			if satoriVersion != "" {
				w.Header().Set("X-Satori-Protocol", satoriVersion)
			}
			next.ServeHTTP(w, request)
		})
	}
}
