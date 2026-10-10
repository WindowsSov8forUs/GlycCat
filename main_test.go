package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type testRootRoutes struct{}

func (testRootRoutes) RegisterRootRoutes(router chi.Router) {
	router.Get("/qqbot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}

func TestQQWebhookListenerAndResponseHeaders(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.WebHook.Host = "::1"
	conf.Account.WebHook.Port = 5141
	webhook := buildQQWebhookServer(conf, testRootRoutes{}, "v1", "GlycCat/test")
	if webhook == nil || webhook.Addr != "[::1]:5141" || webhook.ReadHeaderTimeout == 0 {
		t.Fatalf("webhook listener = %#v", webhook)
	}
	response := httptest.NewRecorder()
	webhook.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/qqbot", nil))
	if response.Code != http.StatusNoContent || response.Header().Get("Server") != "GlycCat/test" ||
		response.Header().Get("X-Satori-Protocol") != "v1" {
		t.Fatalf("response = %d %#v", response.Code, response.Result().Header)
	}
}

func TestQQWebhookSharesSatoriListener(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.WebHook.Host = conf.Satori.Server.Host
	conf.Account.WebHook.Port = conf.Satori.Server.Port
	if server := buildQQWebhookServer(conf, testRootRoutes{}, "v1", "GlycCat/test"); server != nil {
		t.Fatalf("unexpected separate listener: %#v", server)
	}
}

func TestSDKLoggerUsesApplicationLevel(t *testing.T) {
	var output bytes.Buffer
	appLogger := log.GetLogger()
	oldLevel := appLogger.Level
	appLogger.SetOutput(&output)
	defer func() {
		log.SetLogLevel(oldLevel)
		appLogger.SetOutput(os.Stdout)
	}()
	log.SetLogLevel(log.INFO)
	Logger{}.Log(context.Background(), server.LogLevelWarn, "sdk-warning")
	if !strings.Contains(output.String(), "sdk-warning") {
		t.Fatalf("SDK warning was not logged: %q", output.String())
	}
	output.Reset()
	log.SetLogLevel(log.OFF)
	Logger{}.Log(context.Background(), server.LogLevelWarn, "hidden")
	if output.Len() != 0 {
		t.Fatalf("OFF logged %q", output.String())
	}
}

func TestRuntimeStopsBeforeStartupWhenCallbackPortIsBusy(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	bundle := &runtimeBundle{qqWebhookServer: &http.Server{Addr: occupied.Addr().String()}}
	err = bundle.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "监听 QQ 回调地址失败") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRuntimeReturnsSatoriListenFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	srv, err := server.NewServer(server.Config{Host: "127.0.0.1", Port: port, Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bundle := &runtimeBundle{satoriServer: srv}
	if err := bundle.Run(ctx); err == nil {
		t.Fatal("Satori listen failure was not returned")
	}
}

func TestNewRuntimeAcceptsManualWebSocketShard(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.AppID = 123
	conf.Account.AppSecret = "test-secret"
	conf.Account.WebHook.Enable = false
	conf.Account.WebSocket.Enable = true
	conf.Account.WebSocket.Intents = []string{"GROUP_AND_C2C_EVENT"}
	shardID := uint32(0)
	conf.Account.WebSocket.ShardID = &shardID
	conf.Account.WebSocket.ShardCount = 2
	bundle, err := newRuntime(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.satoriServer.Close()
}

func TestNewRuntimeAcceptsMessageStore(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.AppID = 123
	conf.Account.AppSecret = "test-secret"
	store, err := database.OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 50)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bundle, err := newRuntime(conf, store)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.satoriServer.Close()
	if bundle.satoriServer == nil {
		t.Fatal("Satori server was not created")
	}
}
