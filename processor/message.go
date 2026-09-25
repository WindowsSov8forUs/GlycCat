package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (a *Adapter) cacheEvent(evt *event.Event) error {
	if a.store == nil || evt.Login == nil || evt.Login.User == nil || evt.Login.Platform != "qq" {
		return nil
	}
	ch := evt.Channel
	if ch == nil && evt.Message != nil {
		ch = evt.Message.Channel
	}
	if ch == nil && evt.Guild != nil {
		ch = &channel.Channel{Id: evt.Guild.Id, Type: channel.ChannelTypeText}
	}
	if ch == nil {
		return nil
	}
	scope := a.scope(evt.Login.Platform, evt.Login.User.Id, ch.Id)
	switch evt.Type {
	case event.EventTypeGuildAdded, event.EventTypeGuildUpdated, "friend-added":
		return a.store.Observe(scope, ch, evt.Guild, evt.Timestamp, true)
	case event.EventTypeGuildRemoved, "friend-removed":
		return a.store.Observe(scope, ch, evt.Guild, evt.Timestamp, false)
	case event.EventTypeMessageDeleted:
		if evt.Message == nil {
			return nil
		}
		err := a.store.Delete(scope, evt.Message.Id)
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return err
	case event.EventTypeMessageCreated:
		cached, err := copyMessage(evt.Message)
		if err != nil {
			return err
		}
		cached.Channel = ch
		if cached.Guild == nil {
			cached.Guild = evt.Guild
		}
		if cached.User == nil {
			cached.User = evt.User
		}
		if cached.Member == nil {
			cached.Member = evt.Member
		}
		if cached.Referrer == nil {
			cached.Referrer = evt.Referrer
		}
		if cached.User == nil || cached.User.Id == "" {
			return fmt.Errorf("接收消息缺少发送者，不写入不完整缓存")
		}
		return errors.Join(a.store.Save(scope, cached, evt.Timestamp), a.store.Observe(scope, ch, evt.Guild, evt.Timestamp, true))
	default:
		return nil
	}
}

// cacheSent 只缓存 SDK 明确返回的已发送消息，不凭原始请求补造未发送内容
func (a *Adapter) cacheSent(request *server.Request[server.MessageCreateParam], result any) error {
	var sent []*message.Message
	switch value := result.(type) {
	case []*message.Message:
		sent = value
	case *server.Response:
		if value != nil {
			var err error
			sent, err = partialMessages(value.Body)
			if err != nil {
				return err
			}
		}
	case server.Response:
		var err error
		sent, err = partialMessages(value.Body)
		if err != nil {
			return err
		}
	}
	var resultErr error
	for _, item := range sent {
		cached, err := copyMessage(item)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		channelID := request.Params.ChannelID
		direct := strings.HasPrefix(channelID, "private:")
		if value, ok := cached.Referrer["direct"].(bool); ok && value {
			direct = true
		}
		kind := channel.ChannelTypeText
		if direct {
			kind = channel.ChannelTypeDirect
			if !strings.HasPrefix(channelID, "private:") {
				channelID = "private:" + channelID
			}
		}
		cached.Channel = &channel.Channel{Id: channelID, Type: kind}
		if direct {
			cached.Guild = nil
		} else if cached.Guild == nil || cached.Guild.Id != channelID {
			cached.Guild = &guild.Guild{Id: channelID}
		}
		// 部分 QQ 发送响应携带的是接收者，已发送消息的作者始终是当前机器人。
		if cached.User == nil || cached.User.Id != request.SelfID {
			cached.User = &user.User{Id: request.SelfID, IsBot: true}
		} else {
			cached.User.IsBot = true
		}
		scope := a.scope(request.Platform, request.SelfID, channelID)
		timestamp := cached.CreateAt
		if timestamp <= 0 {
			timestamp = time.Now().UnixMilli()
		}
		resultErr = errors.Join(resultErr, a.store.Save(scope, cached, timestamp), a.store.Observe(scope, cached.Channel, cached.Guild, timestamp, true))
	}
	return resultErr
}

func partialMessages(data []byte) ([]*message.Message, error) {
	var body struct {
		Messages []*message.Message `json:"messages"`
	}
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("解析部分发送结果失败: %w", err)
	}
	return body.Messages, nil
}

func copyMessage(value *message.Message) (*message.Message, error) {
	if value == nil {
		return nil, fmt.Errorf("消息内容为空")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result message.Message
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func requestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	}
	return request.Context()
}
