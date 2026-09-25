package database

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

const maxPageSize = 1000

type messageCursor struct {
	Version   int    `json:"v"`
	Namespace string `json:"namespace"`
	Position  string `json:"position"`
}

type pageMessage struct {
	position string
	message  message.Message
	bytes    int
}

// List 按消息时间和稳定 ID 查询，不依赖 QQ 消息 ID 的字典序
// before/after 排除锚点；around 包含存在的锚点，所有方向共享同一个总量限制。
func (s *MessageStore) List(ctx context.Context, scope MessageScope, next, direction, order string, limit int64) (paginated.BidiPaginated[message.Message], error) {
	result := paginated.BidiPaginated[message.Message]{Data: []message.Message{}}
	if s == nil {
		return result, ErrStoreClosed
	}
	prefix, err := scope.messagePrefix()
	if err != nil {
		return result, err
	}
	if direction == "" {
		direction = "before"
	}
	if order == "" {
		order = "asc"
	}
	if (direction != "before" && direction != "after" && direction != "around") || (order != "asc" && order != "desc") || limit < 0 {
		return result, fmt.Errorf("%w: 分页方向、排序或数量无效", ErrInvalid)
	}
	if next == "" && direction != "before" {
		return result, fmt.Errorf("%w: 未提供分页锚点时只能向前查询", ErrInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return result, ErrStoreClosed
	}
	if limit == 0 {
		limit = 50
	}
	if s.limit > 0 && limit > int64(s.limit) {
		limit = int64(s.limit)
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	snapshot, err := s.db.GetSnapshot()
	if err != nil {
		return result, err
	}
	defer snapshot.Release()
	anchor, err := resolveMessageCursor(snapshot, prefix, next)
	if err != nil {
		return result, err
	}
	var items []pageMessage
	if direction == "around" {
		items, result.Prev, result.Next, err = readAround(ctx, snapshot, prefix, anchor, int(limit))
	} else {
		before := direction == "before"
		var more bool
		items, more, _, err = readMessagePage(ctx, snapshot, prefix, anchor, before, int(limit), maxMessageBytes)
		if len(items) > 0 && more {
			cursor := encodeMessageCursor(prefix, items[len(items)-1].position)
			// Satori 的单向查询中 prev 和 next 均指向该查询方向的下一页。
			result.Prev, result.Next = cursor, cursor
		}
		if before {
			reversePage(items)
		}
	}
	if err != nil {
		return result, err
	}
	if order == "desc" {
		reversePage(items)
	}
	for _, item := range items {
		result.Data = append(result.Data, item.message)
	}
	return result, nil
}

func readAround(ctx context.Context, snapshot *leveldb.Snapshot, prefix, anchor string, limit int) ([]pageMessage, string, string, error) {
	var middle []pageMessage
	remainingBytes := maxMessageBytes
	indexKey := "time:" + prefix + anchor
	id, err := snapshot.Get([]byte(indexKey), nil)
	if err == nil {
		item, readErr := readIndexedMessage(snapshot, prefix, anchor, string(id))
		if readErr != nil {
			return nil, "", "", readErr
		}
		middle = append(middle, item)
		remainingBytes -= item.bytes
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return nil, "", "", err
	}
	if remainingBytes < 0 {
		return nil, "", "", ErrCorrupt
	}
	available := limit - len(middle)
	left, moreLeft, leftBytes, err := readMessagePage(ctx, snapshot, prefix, anchor, true, (available+1)/2, remainingBytes)
	if err != nil {
		return nil, "", "", err
	}
	right, moreRight, rightBytes, err := readMessagePage(ctx, snapshot, prefix, anchor, false, available-len(left), remainingBytes-leftBytes)
	if err != nil {
		return nil, "", "", err
	}
	if len(left)+len(right) < available && moreLeft && remainingBytes-rightBytes > leftBytes {
		left, moreLeft, _, err = readMessagePage(ctx, snapshot, prefix, anchor, true, available-len(right), remainingBytes-rightBytes)
		if err != nil {
			return nil, "", "", err
		}
	}
	var prev, next string
	if moreLeft {
		position := anchor
		if len(left) > 0 {
			position = left[len(left)-1].position
		}
		prev = encodeMessageCursor(prefix, position)
	}
	if moreRight {
		position := anchor
		if len(right) > 0 {
			position = right[len(right)-1].position
		}
		next = encodeMessageCursor(prefix, position)
	}
	reversePage(left)
	items := append(left, middle...)
	items = append(items, right...)
	return items, prev, next, nil
}

func readMessagePage(ctx context.Context, snapshot *leveldb.Snapshot, prefix, anchor string, before bool, limit, byteLimit int) ([]pageMessage, bool, int, error) {
	timePrefix := "time:" + prefix
	iter := snapshot.NewIterator(util.BytesPrefix([]byte(timePrefix)), nil)
	defer iter.Release()
	var ok bool
	if anchor == "" {
		if before {
			ok = iter.Last()
		} else {
			ok = iter.First()
		}
	} else {
		key := []byte(timePrefix + anchor)
		ok = iter.Seek(key)
		if before {
			if ok {
				ok = iter.Prev()
			} else {
				ok = iter.Last()
			}
		} else if ok && bytes.Equal(iter.Key(), key) {
			ok = iter.Next()
		}
	}
	items := []pageMessage{}
	totalBytes := 0
	for ok && len(items) < limit {
		if err := ctx.Err(); err != nil {
			return nil, false, 0, err
		}
		position := strings.TrimPrefix(string(iter.Key()), timePrefix)
		item, err := readIndexedMessage(snapshot, prefix, position, string(iter.Value()))
		if err != nil {
			return nil, false, 0, err
		}
		if item.bytes > byteLimit-totalBytes {
			break
		}
		items = append(items, item)
		totalBytes += item.bytes
		if before {
			ok = iter.Prev()
		} else {
			ok = iter.Next()
		}
	}
	if err := iter.Error(); err != nil {
		return nil, false, 0, err
	}
	return items, ok, totalBytes, nil
}

func readIndexedMessage(snapshot *leveldb.Snapshot, prefix, position, messageID string) (pageMessage, error) {
	data, err := snapshot.Get([]byte("message:"+prefix+encodeIdentifier(messageID)), nil)
	if err != nil {
		return pageMessage{}, errors.Join(ErrCorrupt, err)
	}
	record, msg, err := decodeMessageRecord(data)
	if err != nil {
		return pageMessage{}, err
	}
	if msg.Id != messageID || messagePosition(record.Timestamp, messageID) != position || len(record.Message) > maxMessageBytes {
		return pageMessage{}, ErrCorrupt
	}
	return pageMessage{position: position, message: *msg, bytes: len(record.Message)}, nil
}

func resolveMessageCursor(snapshot *leveldb.Snapshot, prefix, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if !strings.HasPrefix(cursor, "gc2:") {
		if !validIdentifier(cursor) {
			return "", fmt.Errorf("%w: 消息锚点无效", ErrInvalid)
		}
		data, err := snapshot.Get([]byte("message:"+prefix+encodeIdentifier(cursor)), nil)
		if errors.Is(err, leveldb.ErrNotFound) {
			return "", ErrNotFound
		}
		if err != nil {
			return "", err
		}
		record, _, err := decodeMessageRecord(data)
		if err != nil {
			return "", err
		}
		return messagePosition(record.Timestamp, cursor), nil
	}
	if len(cursor) > 16384 {
		return "", fmt.Errorf("%w: 分页令牌过长", ErrInvalid)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "gc2:"))
	if err != nil {
		return "", fmt.Errorf("%w: 分页令牌无法解析", ErrInvalid)
	}
	var value messageCursor
	if err := json.Unmarshal(raw, &value); err != nil || value.Version != messageSchemaVersion || value.Namespace != prefix {
		return "", fmt.Errorf("%w: 分页令牌不属于当前账号和会话", ErrInvalid)
	}
	if len(value.Position) < 22 || value.Position[20] != ':' {
		return "", fmt.Errorf("%w: 分页位置无效", ErrInvalid)
	}
	timestamp, err := strconv.ParseInt(value.Position[:20], 10, 64)
	id, decodeErr := base64.RawURLEncoding.DecodeString(value.Position[21:])
	if err != nil || decodeErr != nil || timestamp <= 0 || !validIdentifier(string(id)) || messagePosition(timestamp, string(id)) != value.Position {
		return "", fmt.Errorf("%w: 分页位置无效", ErrInvalid)
	}
	return value.Position, nil
}

func encodeMessageCursor(prefix, position string) string {
	data, _ := json.Marshal(messageCursor{Version: messageSchemaVersion, Namespace: prefix, Position: position})
	return "gc2:" + base64.RawURLEncoding.EncodeToString(data)
}

func reversePage(items []pageMessage) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}
