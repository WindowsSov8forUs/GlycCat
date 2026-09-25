package processor

import (
	"path/filepath"
	"testing"

	"github.com/WindowsSov8forUs/glyccat/database"
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
