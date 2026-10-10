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

func TestManualShardRequiresValidPair(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    *uint32
		count uint32
		valid bool
	}{
		{name: "auto", valid: true},
		{name: "missing count", id: shardID(0)},
		{name: "missing id", count: 2},
		{name: "out of range", id: shardID(2), count: 2},
		{name: "manual", id: shardID(1), count: 2, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := DefaultConfig()
			conf.Account.AppID = 123
			conf.Account.AppSecret = "test-secret"
			conf.Account.WebHook.Enable = false
			conf.Account.WebSocket.Enable = true
			conf.Account.WebSocket.Intents = []string{"GROUP_AND_C2C_EVENT"}
			conf.Account.WebSocket.ShardID = tc.id
			conf.Account.WebSocket.ShardCount = tc.count
			err := conf.NormalizeAndValidate()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}

func shardID(value uint32) *uint32 { return &value }
