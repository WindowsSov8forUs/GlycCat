package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

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
