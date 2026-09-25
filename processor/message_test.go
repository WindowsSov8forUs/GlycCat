package processor

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func TestCacheSentUsesBotAsAuthor(t *testing.T) {
	store, err := database.OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adapter := &Adapter{store: store, appID: "123"}
	request := &server.Request[server.MessageCreateParam]{
		Platform: "qq", SelfID: "bot", Params: server.MessageCreateParam{ChannelID: "group"},
	}
	result := []*message.Message{{Id: "m1", CreateAt: 1234, User: &user.User{Id: "recipient"}}}
	if err := adapter.cacheSent(request, result); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(database.MessageScope{AppID: "123", Platform: "qq", SelfID: "bot", ChannelID: "group"}, "m1")
	if err != nil || got.User.Id != "bot" || !got.User.IsBot {
		t.Fatalf("cached author = %#v, %v", got, err)
	}
}

func TestCacheEventUsesLoginIdentity(t *testing.T) {
	store, err := database.OpenMessageStore(filepath.Join(t.TempDir(), "messages"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adapter := &Adapter{store: store, appID: "app-a"}
	ch := &channel.Channel{Id: "group", Type: channel.ChannelTypeText}
	evt := &event.Event{
		Type: event.EventTypeMessageCreated, Timestamp: 1234,
		Login:   &login.Login{Platform: "qq", User: &user.User{Id: "bot-a"}},
		Channel: ch, Message: &message.Message{Id: "m1", Content: "hello", User: &user.User{Id: "sender"}},
	}
	if err := adapter.cacheEvent(evt); err != nil {
		t.Fatal(err)
	}
	scope := database.MessageScope{AppID: "app-a", Platform: "qq", SelfID: "bot-a", ChannelID: "group"}
	got, err := store.Get(scope, "m1")
	if err != nil || got.Content != "hello" || got.User.Id != "sender" {
		t.Fatalf("cached message = %#v, %v", got, err)
	}
	other := scope
	other.SelfID = "bot-b"
	if _, err := store.Get(other, "m1"); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("other login unexpectedly sees cached message: %v", err)
	}
}
