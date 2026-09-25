package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/WindowsSov8forUs/glyccat/fileserver"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/WindowsSov8forUs/glyccat/sys"
	"github.com/WindowsSov8forUs/glyccat/version"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func main() {
	fastStart := flag.Bool("faststart", false, "fast startup")
	debug := flag.Bool("debug", false, "debug mode")
	flag.Parse()

	if !*fastStart {
		sys.InitBase()
	}

	fmt.Println(version.Logo())
	versionString := log.StringCenter(fmt.Sprintf("GlycCat %s", version.Version), 58)
	log.PrintlnCyan(versionString)
	fmt.Print("\n==========================================================\n\n")

	conf, err := config.LoadConfig("config.yml")
	if err != nil {
		fmt.Printf("%s load config failed: %v\n", log.FailMark, log.Red(fmt.Sprint(err)))
		os.Exit(0)
		return
	}

	log.SetLogLevel(conf.LogLevel)

	if *debug {
		log.Warn("running in debug mode")
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	log.GetLogger()

	if conf.Account.Token == "" {
		log.Fatal("bot token is empty, please configure account token")
		os.Exit(0)
		return
	}

	if err := log.Start(); err != nil {
		fmt.Printf("start log failed: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	fileserver.StartFileServer(conf)

	if conf.Database.MessageDatabase.Enable {
		log.Info("starting message database")
		err := database.StartMessageDB(conf.Database.MessageDatabase.Limit)
		if err != nil {
			log.Errorf("start message database failed: %v", err)
		}
	} else {
		log.Warn("message database is disabled")
	}

	runtime, err := newRuntime(conf)
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

func newRuntime(conf *config.Config) (*runtimeBundle, error) {
	useWebSocket := conf.Account.WebSocket.Enable && !conf.Account.WebHook.Enable
	if !useWebSocket && !conf.Account.WebHook.Enable {
		return nil, fmt.Errorf("both webhook and websocket are disabled")
	}

	adapterCfg := qq.Config{
		AppID:         conf.Account.AppID,
		Secret:        conf.Account.AppSecret,
		Token:         conf.Account.Token,
		Sandbox:       conf.Account.Sandbox,
		Path:          conf.Account.WebHook.Path,
		Adapter:       "GlycCat",
		UseWebSocket:  useWebSocket,
		WSIntentNames: conf.Account.WebSocket.Intents,
		WSShardCount:  conf.Account.WebSocket.Shards,
	}

	innerAdapter, err := qq.New(adapterCfg)
	if err != nil {
		return nil, err
	}

	version := conf.Satori.Version
	if version == 0 {
		version = 1
	}
	srv, err := server.NewServer(server.Config{
		Host:    conf.Satori.Server.Host,
		Port:    int(conf.Satori.Server.Port),
		Path:    conf.Satori.Path,
		Version: fmt.Sprintf("v%d", version),
		Token:   conf.Satori.Token,
	})
	if err != nil {
		return nil, err
	}
	if applyErr := srv.Apply(innerAdapter); applyErr != nil {
		return nil, applyErr
	}

	webhookServer := buildQQWebhookServer(conf, innerAdapter)

	return &runtimeBundle{
		satoriServer:    srv,
		qqWebhookServer: webhookServer,
	}, nil
}

func buildQQWebhookServer(conf *config.Config, registrar server.RootRouteRegistrar) *http.Server {
	if !conf.Account.WebHook.Enable {
		return nil
	}

	webhookHost := strings.TrimSpace(conf.Account.WebHook.Host)
	if webhookHost == "" {
		webhookHost = strings.TrimSpace(conf.Satori.Server.Host)
	}
	webhookPort := conf.Account.WebHook.Port
	if webhookPort == 0 {
		webhookPort = conf.Satori.Server.Port
	}

	satoriHost := strings.TrimSpace(conf.Satori.Server.Host)
	if satoriHost == "" {
		satoriHost = "127.0.0.1"
	}
	satoriPort := conf.Satori.Server.Port
	if satoriPort == 0 {
		satoriPort = 5500
	}

	if isSameListenEndpoint(webhookHost, webhookPort, satoriHost, satoriPort) {
		return nil
	}

	router := chi.NewRouter()
	registrar.RegisterRootRoutes(router)

	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", webhookHost, webhookPort),
		Handler: router,
	}
}

func isSameListenEndpoint(hostA string, portA uint16, hostB string, portB uint16) bool {
	if portA != portB {
		return false
	}
	normalize := func(host string) string {
		host = strings.TrimSpace(host)
		if host == "" || host == "::" {
			return "0.0.0.0"
		}
		return host
	}
	a := normalize(hostA)
	b := normalize(hostB)
	if a == b {
		return true
	}
	return a == "0.0.0.0" || b == "0.0.0.0"
}
