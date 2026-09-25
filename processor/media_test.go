package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	stdimage "image"
	"image/png"
	"net/http/httptest"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

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
