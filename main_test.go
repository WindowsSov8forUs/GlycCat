package main

import (
	"path/filepath"
	"testing"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/WindowsSov8forUs/glyccat/database"
)

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
