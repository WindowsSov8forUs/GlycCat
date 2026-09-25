package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
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
