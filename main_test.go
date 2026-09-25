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
