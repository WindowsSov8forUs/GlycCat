package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesAndUpdateKeepsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	original := []byte("account:\n  app_id: 123\n  app_secret: secret\n  websocket:\n    enable: true\n    intents: []\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Account.AppID != 123 || !conf.Account.WebSocket.Enable || conf.Account.WebHook.Enable || len(conf.Account.WebSocket.Intents) != 6 {
		t.Fatalf("migrated config = %#v", conf.Account)
	}
	loaded, err := os.ReadFile(path)
	if err != nil || bytes.Equal(loaded, original) {
		t.Fatalf("LoadConfig did not migrate the legacy config: %v", err)
	}
	if bytes.Count(loaded, []byte("      - ")) != 6 || bytes.Count(loaded, []byte("      # - ")) != len(intentOptions)-6 {
		t.Fatalf("intent selections or comments were lost: %s", loaded)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("migration backups = %v, error = %v", backups, err)
	}
	backedUp, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backedUp, original) {
		t.Fatalf("backup differs from original: %v", err)
	}
	backup, err := UpdateConfig(path)
	if err != nil || backup != "" {
		t.Fatalf("unchanged config was rewritten: backup = %q, error = %v", backup, err)
	}
	updated, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(updated, loaded) {
		t.Fatalf("UpdateConfig changed the migrated config: %v", err)
	}
	if _, err := decodeConfig(updated); err != nil {
		t.Fatalf("updated config is invalid: %v", err)
	}
}

func TestDecodeConfigRejectsUnknownFields(t *testing.T) {
	_, err := decodeConfig([]byte("config_version: 1\naccount:\n  app_id: 123\n  app_secret: secret\nunknown: true\n"))
	if err == nil {
		t.Fatal("unknown config field was accepted")
	}
}
