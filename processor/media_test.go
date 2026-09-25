package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	stdimage "image"
	"image/png"
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
