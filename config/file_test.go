package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIsReadOnlyAndUpdateKeepsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	original := []byte("account:\n  app_id: 123\n  app_secret: secret\n  token: legacy-token\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Account.AppID != 123 || !conf.Account.WebHook.Enable {
		t.Fatalf("loaded defaults = %#v", conf.Account)
	}
	loaded, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(loaded, original) {
		t.Fatalf("LoadConfig changed file: %v", err)
	}
	backup, err := UpdateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	backedUp, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(backedUp, original) {
		t.Fatalf("backup differs from original: %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil || bytes.Equal(updated, original) {
		t.Fatalf("UpdateConfig did not rewrite config: %v", err)
	}
	if _, err := decodeConfig(updated); err != nil {
		t.Fatalf("updated config is invalid: %v", err)
	}
}

func TestDecodeConfigRejectsUnknownFields(t *testing.T) {
	_, err := decodeConfig([]byte("account:\n  app_id: 123\n  app_secret: secret\n  token: legacy-token\nunknown: true\n"))
	if err == nil {
		t.Fatal("unknown config field was accepted")
	}
}
