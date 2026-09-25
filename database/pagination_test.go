package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

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
