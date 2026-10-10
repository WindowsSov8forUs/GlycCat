package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	stdimage "image"
	"image/png"
	"net/http/httptest"
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

func TestNormalizeLegacyQuotesPreservesOtherContent(t *testing.T) {
	input := `<quote data-x='1'><message id="old"/></quote><text>keep &amp; this</text>`
	want := `<quote data-x='1' id="old"><message id="old"/></quote><text>keep &amp; this</text>`
	got, err := normalizeLegacyQuotes(context.Background(), input)
	if err != nil || got != want {
		t.Fatalf("normalizeLegacyQuotes() = %q, %v", got, err)
	}
	explicit := `<quote id="new"><message id="old"/></quote>`
	got, err = normalizeLegacyQuotes(context.Background(), explicit)
	if err != nil || got != explicit {
		t.Fatalf("explicit quote changed to %q: %v", got, err)
	}
}

func TestPrepareMessageMediaKeepsCompatibleImageAndQuote(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData.Bytes())
	content := `<quote><img src="data:invalid"/></quote><img src="` + src + `" title="keep.png"/>`
	adapter := &Adapter{closed: context.Background()}
	request := &server.Request[server.MessageCreateParam]{
		Platform: "qq", SelfID: "bot", Params: server.MessageCreateParam{ChannelID: "group", Content: content},
	}
	got, err := adapter.prepareMessageMedia(request)
	if err != nil || got != content {
		t.Fatalf("prepareMessageMedia() = %q, %v", got, err)
	}
}

func TestMessageCreatePreparesContentBeforeSending(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData.Bytes())
	var sent string
	adapter := &Adapter{closed: context.Background(), routes: map[string]server.RouteCall[any, any]{
		"message.create": func(request *server.Request[any]) (any, error) {
			params, ok := request.Params.(server.MessageCreateParam)
			if !ok {
				t.Fatalf("forwarded params type = %T", request.Params)
			}
			sent = params.Content
			return "sent", nil
		},
	}}
	adapter.registerCacheRoutes()
	content := `<quote><message id="old"/></quote><img src="` + src + `"/>`
	request := &server.Request[any]{
		Origin: httptest.NewRequest("POST", "/v1/message.create", nil), Platform: "qq", SelfID: "bot",
		Params: server.MessageCreateParam{ChannelID: "group", Content: content},
	}
	if _, err := adapter.routes["message.create"](request); err != nil {
		t.Fatal(err)
	}
	want := `<quote id="old"><message id="old"/></quote><img src="` + src + `"/>`
	if sent != want {
		t.Fatalf("forwarded content = %q, want %q", sent, want)
	}
	sent = ""
	request.Params = server.MessageCreateParam{ChannelID: "group", Content: `<img src="data:invalid"/>`}
	if _, err := adapter.routes["message.create"](request); err == nil || sent != "" {
		t.Fatalf("invalid media was sent: content=%q, err=%v", sent, err)
	}
}

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
