package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
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
