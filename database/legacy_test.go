package database

import (
	"bytes"
	"context"
	"encoding/gob"
	"path/filepath"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/syndtr/goleveldb/leveldb"
)

func TestMigrateMessagesIsExplicitAndRepeatable(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy")
	targetPath := filepath.Join(dir, "new")
	source, err := leveldb.OpenFile(sourcePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	msg := &message.Message{Id: "m1", CreateAt: 1234, User: &user.User{Id: "sender"}}
	if err := gob.NewEncoder(&encoded).Encode(msg); err != nil {
		t.Fatal(err)
	}
	if err := source.Put([]byte("group:ch:m1"), encoded.Bytes(), nil); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateMessages(context.Background(), sourcePath, targetPath, "", "bot"); err == nil {
		t.Fatal("migration accepted missing AppID")
	}
	first, err := MigrateMessages(context.Background(), sourcePath, targetPath, "123", "bot")
	if err != nil || first.Read != 1 || first.Imported != 1 {
		t.Fatalf("first migration = %#v, %v", first, err)
	}
	second, err := MigrateMessages(context.Background(), sourcePath, targetPath, "123", "bot")
	if err != nil || second.Read != 1 || second.Skipped != 1 {
		t.Fatalf("repeated migration = %#v, %v", second, err)
	}
	store, err := OpenMessageStore(targetPath, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Get(MessageScope{AppID: "123", Platform: "qq", SelfID: "bot", ChannelID: "ch"}, "m1")
	if err != nil || got.User.Id != "sender" {
		t.Fatalf("migrated message = %#v, %v", got, err)
	}
}
