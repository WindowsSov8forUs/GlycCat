package main

import (
	"testing"

	"github.com/WindowsSov8forUs/glyccat/config"
)

func TestSDKRuntimeDoesNotRequireLegacyToken(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.AppID = 123
	conf.Account.AppSecret = "test-secret"
	bundle, err := newRuntime(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.satoriServer.Close()
	if bundle.satoriServer == nil {
		t.Fatal("Satori server was not created")
	}
}
