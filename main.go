package main

import (
	"context"
	"errors"
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

type Logger struct{}

func (Logger) Log(_ context.Context, level server.LogLevel, v ...any) {
	if len(v) == 0 {
		v = []any{"Satori 服务事件"}
	}
	lvl := log.INFO
	switch level {
	case server.LogLevelDebug:
		lvl = log.DEBUG
	case server.LogLevelWarn:
		lvl = log.WARN
	case server.LogLevelError:
		lvl = log.ERROR
	}
	log.GetLogger().Println(lvl, v...)
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "程序运行失败: %v\n", err)
	}
	closeErr := log.Close()
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "关闭日志失败: %v\n", closeErr)
	}
	if err != nil || closeErr != nil {
		os.Exit(1)
	}
}

func run() (runErr error) {
	fastStart := flag.Bool("faststart", false, "跳过启动环境提示")
	debug := flag.Bool("debug", false, "启用调试日志")
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
		return fmt.Errorf("初始化配置、迁移配置和迁移消息不能同时执行")
	}
	if *legacyMessages != "" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		result, err := database.MigrateMessages(ctx, *legacyMessages, *migrationTarget, *migrationAppID, *migrationSelfID)
		fmt.Printf("消息迁移结果：读取 %d 条，导入 %d 条，已存在 %d 条，无法迁移 %d 条\n", result.Read, result.Imported, result.Skipped, result.Failed)
		return err
	}
	if *initialize {
		if err := config.InitializeConfig(*configPath); err != nil {
			return err
		}
		fmt.Printf("配置已保存至 %s\n", *configPath)
		return nil
	}
	if *updateConfig {
		backup, err := config.UpdateConfig(*configPath)
		if backup != "" {
			fmt.Printf("原配置备份: %s\n", backup)
		}
		return err
	}
	if !*fastStart {
		sys.InitBase()
	}

	fmt.Println(version.Logo())
	log.PrintlnCyan(log.StringCenter(fmt.Sprintf("GlycCat %s", version.Version), 58))
	fmt.Print("\n==========================================================\n\n")

	conf, err := config.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	log.SetLogLevel(conf.LogLevel)
	if *debug {
		log.SetLogLevel(log.DEBUG)
	}
	if err := log.Start(); err != nil {
		return err
	}
	// 原生 SDK 使用进程级日志入口，仅在主程序启动时注册一次。
	qq.RegisterSDKLogger(Logger{})
	if conf.FileServer.Enable {
		log.Warn("旧文件服务器已退出普通发送链路，媒体请使用 upload.create；原文件数据保持不变")
	}
	var messageStore *database.MessageStore
	if conf.Database.MessageDatabase.Enable {
		messageStore, err = database.OpenMessageStore(database.DefaultMessageStorePath, conf.Database.MessageDatabase.Limit)
		if err != nil {
			return fmt.Errorf("启动消息数据库失败: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, messageStore.Close()) }()
	} else {
		log.Warn("消息数据库未启用，群聊和私聊历史缓存不可用")
	}

	bundle, err := newRuntime(conf, messageStore)
	if err != nil {
		return fmt.Errorf("初始化运行环境失败: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, bundle.satoriServer.Close()) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return bundle.Run(ctx)
}

type runtimeBundle struct {
	satoriServer    *server.Server
	qqWebhookServer *http.Server
}

// Run 管理两路监听与退出，任何启动失败都会结束本轮运行
func (r *runtimeBundle) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var callbackListener net.Listener
	var err error
	if r.qqWebhookServer != nil {
		callbackListener, err = net.Listen("tcp", r.qqWebhookServer.Addr)
		if err != nil {
			return fmt.Errorf("监听 QQ 回调地址失败: %w", err)
		}
		defer callbackListener.Close()
		log.Infof("QQ 回调监听地址: %s", callbackListener.Addr())
	}

	satoriResult := make(chan error, 1)
	callbackResult := make(chan error, 1)
	go func() { satoriResult <- r.satoriServer.Run(runCtx) }()
	if callbackListener != nil {
		go func() { callbackResult <- r.qqWebhookServer.Serve(callbackListener) }()
	}
	log.Infof("Satori 服务地址: %s", r.satoriServer.URLBase())

	var runErr error
	satoriFinished := false
	select {
	case <-ctx.Done():
		log.Info("收到退出信号，正在关闭服务")
	case runErr = <-satoriResult:
		satoriFinished = true
	case runErr = <-callbackResult:
		if errors.Is(runErr, http.ErrServerClosed) {
			runErr = nil
		}
	}

	cancel()
	if r.qqWebhookServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := r.qqWebhookServer.Shutdown(shutdownCtx)
		shutdownCancel()
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, r.qqWebhookServer.Close())
		}
		runErr = errors.Join(runErr, shutdownErr)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	runErr = errors.Join(runErr, r.satoriServer.Shutdown(shutdownCtx))
	if !satoriFinished {
		select {
		case result := <-satoriResult:
			runErr = errors.Join(runErr, result)
		case <-shutdownCtx.Done():
			runErr = errors.Join(runErr, fmt.Errorf("等待 Satori 服务退出超时: %w", shutdownCtx.Err()))
		}
	}
	return runErr
}

func newRuntime(conf *config.Config, messageStore *database.MessageStore) (bundle *runtimeBundle, err error) {
	if err := conf.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	var logger = Logger{}
	adapterCfg := qq.Config{
		AppID:         conf.Account.AppID,
		Secret:        conf.Account.AppSecret,
		Sandbox:       conf.Account.Sandbox,
		Path:          conf.Account.WebHook.Path,
		Adapter:       "GlycCat",
		UseWebSocket:  conf.Account.WebSocket.Enable,
		WSIntentNames: conf.Account.WebSocket.Intents,
		WSShardCount:  conf.Account.WebSocket.ShardCount,
		Logger:        logger,
	}
	if conf.Account.WebSocket.ShardID != nil {
		adapterCfg.WSShardID = *conf.Account.WebSocket.ShardID
	}
	innerAdapter, err := qq.New(adapterCfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, innerAdapter.Cleanup(context.Background()))
		}
	}()

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
		Logger:        logger,
		// 此客户端只用于 Satori 反向推送，不改变 QQ 请求或资源代理的超时。
		// 旧配置的 0 不追加应用上限，仍遵守 SDK 的单订阅超时，不表示无限等待。
		HTTPClient: &http.Client{Timeout: time.Duration(conf.Satori.WebHook.Timeout) * time.Second},
	})
	if err != nil {
		return nil, err
	}
	appAdapter, err := processor.NewAdapter(innerAdapter, messageStore, strconv.FormatUint(conf.Account.AppID, 10))
	if err != nil {
		return nil, errors.Join(err, srv.Close())
	}
	if applyErr := srv.Apply(appAdapter); applyErr != nil {
		return nil, errors.Join(applyErr, appAdapter.Cleanup(context.Background()), srv.Close())
	}
	return &runtimeBundle{
		satoriServer:    srv,
		qqWebhookServer: buildQQWebhookServer(conf, appAdapter, satoriVersion, serverHeader),
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
