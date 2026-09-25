package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/syndtr/goleveldb/leveldb"
)

func TestMessageMigrationCommandDoesNotLoadRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy")
	targetPath := filepath.Join(dir, "messages-v2")
	source, err := leveldb.OpenFile(sourcePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(&message.Message{Id: "m1", CreateAt: 1234, User: &user.User{Id: "sender"}}); err != nil {
		t.Fatal(err)
	}
	if err := source.Put([]byte("group:ch:m1"), encoded.Bytes(), nil); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", ".", "-migrate-messages", sourcePath,
		"-migration-target", targetPath, "-migration-app-id", "123", "-migration-self-id", "bot",
		"-config", filepath.Join(dir, "missing-config.yml"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("migration command failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "导入 1 条") {
		t.Fatalf("unexpected migration report: %s", output)
	}
	store, err := database.OpenMessageStore(targetPath, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Get(database.MessageScope{AppID: "123", Platform: "qq", SelfID: "bot", ChannelID: "ch"}, "m1")
	if err != nil || got.User.Id != "sender" {
		t.Fatalf("migrated message = %#v, %v", got, err)
	}
}
