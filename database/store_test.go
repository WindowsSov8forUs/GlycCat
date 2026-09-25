package database

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

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
