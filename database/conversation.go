package database

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// Conversation 已观察到的会话，不代表 QQ 平台的完整会话列表
type Conversation struct {
	SchemaVersion int              `json:"schema_version"`
	Channel       *channel.Channel `json:"channel"`
	Guild         *guild.Guild     `json:"guild,omitempty"`
	Timestamp     int64            `json:"timestamp"`
	Active        bool             `json:"active"`
}

// Observe 更新会话目录，旧事件不会覆盖更新的退出状态
func (s *MessageStore) Observe(scope MessageScope, ch *channel.Channel, group *guild.Guild, timestamp int64, active bool) error {
	if s == nil {
		return ErrStoreClosed
	}
	owner, err := scope.ownerPrefix()
	if err != nil {
		return err
	}
	if ch == nil || !validIdentifier(ch.Id) || ch.Id != scope.ChannelID {
		return fmt.Errorf("%w: 会话信息不完整", ErrInvalid)
	}
	if timestamp <= 0 {
		timestamp = time.Now().UnixMilli()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrStoreClosed
	}
	key := []byte("conversation:" + owner + encodeIdentifier(ch.Id))
	oldData, err := s.db.Get(key, nil)
	var previous *Conversation
	if err == nil {
		previous, err = decodeConversation(oldData)
		if err != nil {
			return err
		}
		if previous.Timestamp > timestamp {
			return nil
		}
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	// 复制输入，避免目录更新改变正在转发的 SDK 事件。
	copied := *ch
	if previous != nil && copied.Name == "" {
		copied.Name = previous.Channel.Name
	}
	if group == nil && previous != nil {
		group = previous.Guild
	}
	if copied.Type == channel.ChannelTypeDirect {
		group = nil
	}
	item := &Conversation{SchemaVersion: messageSchemaVersion, Channel: &copied, Guild: group, Timestamp: timestamp, Active: active}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return fmt.Errorf("%w: 会话元数据超过上限", ErrInvalid)
	}
	batch := new(leveldb.Batch)
	batch.Put(key, data)
	groupKey := []byte("guild:" + owner + encodeIdentifier(ch.Id))
	if active && copied.Type != channel.ChannelTypeDirect {
		batch.Put(groupKey, data)
	} else {
		batch.Delete(groupKey)
	}
	return s.db.Write(batch, nil)
}

// GetConversation 只返回仍处于已观察活动状态的会话
func (s *MessageStore) GetConversation(scope MessageScope) (*Conversation, error) {
	if s == nil {
		return nil, ErrStoreClosed
	}
	owner, err := scope.ownerPrefix()
	if err != nil {
		return nil, err
	}
	if !validIdentifier(scope.ChannelID) {
		return nil, fmt.Errorf("%w: 会话 ID 无效", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrStoreClosed
	}
	data, err := s.db.Get([]byte("conversation:"+owner+encodeIdentifier(scope.ChannelID)), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item, err := decodeConversation(data)
	if err != nil {
		return nil, err
	}
	if item.Channel.Id != scope.ChannelID {
		return nil, ErrCorrupt
	}
	if !item.Active {
		return nil, ErrNotFound
	}
	return item, nil
}

// ListGuilds 返回已观察群组的分页列表，私聊不进入群组索引
func (s *MessageStore) ListGuilds(ctx context.Context, scope MessageScope, next string) (paginated.Paginated[guild.Guild], error) {
	result := paginated.Paginated[guild.Guild]{Data: []guild.Guild{}}
	if s == nil {
		return result, ErrStoreClosed
	}
	owner, err := scope.ownerPrefix()
	if err != nil {
		return result, err
	}
	var position string
	if next != "" {
		if len(next) > 16384 || !strings.HasPrefix(next, "gc2g:") {
			return result, fmt.Errorf("%w: 群组分页令牌无效", ErrInvalid)
		}
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(next, "gc2g:"))
		if err != nil {
			return result, fmt.Errorf("%w: 群组分页令牌无法解析", ErrInvalid)
		}
		var cursor messageCursor
		if err := json.Unmarshal(data, &cursor); err != nil || cursor.Version != messageSchemaVersion || cursor.Namespace != owner {
			return result, fmt.Errorf("%w: 群组分页令牌不属于当前账号", ErrInvalid)
		}
		id, err := base64.RawURLEncoding.DecodeString(cursor.Position)
		if err != nil || !validIdentifier(string(id)) || encodeIdentifier(string(id)) != cursor.Position {
			return result, fmt.Errorf("%w: 群组分页位置无效", ErrInvalid)
		}
		position = cursor.Position
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return result, ErrStoreClosed
	}
	prefix := "guild:" + owner
	iter := s.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()
	ok := iter.First()
	if position != "" {
		ok = iter.Seek([]byte(prefix + position))
		if ok && string(iter.Key()) == prefix+position {
			ok = iter.Next()
		}
	}
	var last string
	for ok {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if len(result.Data) == 50 {
			data, _ := json.Marshal(messageCursor{Version: messageSchemaVersion, Namespace: owner, Position: last})
			result.Next = "gc2g:" + base64.RawURLEncoding.EncodeToString(data)
			break
		}
		item, err := decodeConversation(iter.Value())
		if err != nil {
			return result, err
		}
		if !item.Active || item.Channel.Type == channel.ChannelTypeDirect || string(iter.Key()) != prefix+encodeIdentifier(item.Channel.Id) {
			return result, ErrCorrupt
		}
		group := guild.Guild{Id: item.Channel.Id}
		if item.Guild != nil {
			group = *item.Guild
		}
		result.Data = append(result.Data, group)
		last = strings.TrimPrefix(string(iter.Key()), prefix)
		ok = iter.Next()
	}
	if err := iter.Error(); err != nil {
		return result, err
	}
	return result, nil
}

func decodeConversation(data []byte) (*Conversation, error) {
	var item Conversation
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, errors.Join(ErrCorrupt, err)
	}
	if item.SchemaVersion != messageSchemaVersion || item.Channel == nil || !validIdentifier(item.Channel.Id) {
		return nil, ErrCorrupt
	}
	return &item, nil
}
