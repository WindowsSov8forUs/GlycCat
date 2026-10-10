package database

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/syndtr/goleveldb/leveldb"
)

func TestConversationExitBeatsStaleJoin(t *testing.T) {
	store, err := OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := MessageScope{AppID: "app-a", Platform: "qq", SelfID: "bot", ChannelID: "group"}
	ch := &channel.Channel{Id: "group", Type: channel.ChannelTypeText}
	group := &guild.Guild{Id: "group", Name: "name"}
	if err := store.Observe(scope, ch, group, 100, true); err != nil {
		t.Fatal(err)
	}
	groups, err := store.ListGuilds(context.Background(), scope, "")
	if err != nil || len(groups.Data) != 1 || groups.Data[0].Id != "group" {
		t.Fatalf("active groups = %#v, %v", groups, err)
	}
	if err := store.Observe(scope, ch, nil, 200, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(scope, ch, group, 100, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConversation(scope); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exited conversation error = %v", err)
	}
	groups, err = store.ListGuilds(context.Background(), scope, "")
	if err != nil || len(groups.Data) != 0 {
		t.Fatalf("groups after exit = %#v, %v", groups, err)
	}
}

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
	before := snapshotDatabaseFiles(t, sourcePath)
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
	if after := snapshotDatabaseFiles(t, sourcePath); !reflect.DeepEqual(before, after) {
		t.Fatal("migration modified source database files")
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

func TestMigrateMessagesRejectsOverlappingDirectories(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy")
	if err := os.Mkdir(sourcePath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{sourcePath, filepath.Join(sourcePath, "new"), dir} {
		if _, err := MigrateMessages(context.Background(), sourcePath, target, "123", "bot"); err == nil {
			t.Fatalf("accepted overlapping target %q", target)
		}
	}
}

func snapshotDatabaseFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestMessageListUsesTimeAndScopedCursor(t *testing.T) {
	store, err := OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := MessageScope{AppID: "app-a", Platform: "qq", SelfID: "bot", ChannelID: "group"}
	for i, timestamp := range []int64{300, 100, 200} {
		msg := &message.Message{Id: fmt.Sprintf("m%d", i), CreateAt: timestamp, Channel: &channel.Channel{Id: "group"}}
		if err := store.Save(scope, msg, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.List(context.Background(), scope, "", "before", "asc", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 2 || first.Data[0].Id != "m2" || first.Data[1].Id != "m0" || first.Next == "" {
		t.Fatalf("first page = %#v", first)
	}
	second, err := store.List(context.Background(), scope, first.Next, "before", "asc", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Data) != 1 || second.Data[0].Id != "m1" {
		t.Fatalf("second page = %#v", second)
	}
	other := scope
	other.AppID = "app-b"
	if _, err := store.List(context.Background(), other, first.Next, "before", "asc", 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-account cursor error = %v", err)
	}
}

func TestMessageStoreKeepsAccountScopesSeparate(t *testing.T) {
	store, err := OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := MessageScope{AppID: "app-a", Platform: "qq", SelfID: "bot", ChannelID: "group"}
	msg := &message.Message{Id: "m1", CreateAt: 1234, Channel: &channel.Channel{Id: "group"}}
	if err := store.Save(scope, msg, 1234); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(scope, "m1")
	if err != nil || got.Id != msg.Id {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
	for name, change := range map[string]func(*MessageScope){
		"app":      func(other *MessageScope) { other.AppID = "app-b" },
		"platform": func(other *MessageScope) { other.Platform = "qqguild" },
		"self":     func(other *MessageScope) { other.SelfID = "another-bot" },
		"channel":  func(other *MessageScope) { other.ChannelID = "another-group" },
	} {
		t.Run(name, func(t *testing.T) {
			other := scope
			change(&other)
			if _, err := store.Get(other, "m1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("other scope Get() error = %v", err)
			}
		})
	}
	if err := store.Delete(scope, "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(scope, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted message Get() error = %v", err)
	}
}
