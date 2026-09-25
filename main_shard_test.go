package main

import (
	"testing"

	"github.com/WindowsSov8forUs/glyccat/config"
)

func TestNewRuntimeAcceptsManualWebSocketShard(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.AppID = 123
	conf.Account.AppSecret = "test-secret"
	conf.Account.WebHook.Enable = false
	conf.Account.WebSocket.Enable = true
	shardID := uint32(0)
	conf.Account.WebSocket.ShardID = &shardID
	conf.Account.WebSocket.ShardCount = 2
	bundle, err := newRuntime(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.satoriServer.Close()
}
