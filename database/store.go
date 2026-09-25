package database

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

const (
	DefaultMessageStorePath = "data/db/messages-v2"
	messageSchemaVersion    = 2
	maxMessageBytes         = 32 * 1024 * 1024
)

var (
	ErrStoreClosed = errors.New("消息数据库未开启或已关闭")
	ErrNotFound    = errors.New("未找到缓存记录")
	ErrInvalid     = errors.New("缓存请求参数无效")
	ErrCorrupt     = errors.New("缓存记录损坏")
)

// MessageScope 消息所属账号与会话，不使用临时登录序号作为账号标识
// AppID、Platform、SelfID 和 ChannelID 必须共同参与消息键的构造。
type MessageScope struct {
	AppID     string
	Platform  string
	SelfID    string
	ChannelID string
}

// MessageStore 独立的应用消息库，生命周期由主程序管理
// SDK 的消息通过 JSON 保存，避免丢失字段存在性与嵌套回复上下文。
type MessageStore struct {
	mu    sync.RWMutex
	db    *leveldb.DB
	limit int
}

type messageRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Timestamp     int64           `json:"timestamp"`
	Message       json.RawMessage `json:"message"`
}

// OpenMessageStore 打开新格式数据库，不在原消息库上进行隐式迁移
func OpenMessageStore(path string, limit int) (*MessageStore, error) {
	if limit < 0 {
		return nil, fmt.Errorf("%w: 查询上限不能为负数", ErrInvalid)
	}
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, err
	}
	version, err := db.Get([]byte("schema_version"), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		iter := db.NewIterator(nil, nil)
		nonempty := iter.First()
		iterErr := iter.Error()
		iter.Release()
		if nonempty || iterErr != nil {
			_ = db.Close()
			return nil, errors.Join(fmt.Errorf("目标数据库包含未知格式的数据，请保留原库并显式迁移"), iterErr)
		}
		err = db.Put([]byte("schema_version"), []byte(strconv.Itoa(messageSchemaVersion)), &opt.WriteOptions{Sync: true})
	} else if err == nil && string(version) != strconv.Itoa(messageSchemaVersion) {
		err = fmt.Errorf("消息数据库版本不兼容，请保留原库后使用对应版本迁移")
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &MessageStore{db: db, limit: limit}, nil
}

// Close 等待正在执行的缓存操作结束，再关闭数据库
func (s *MessageStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Save 保存消息与时间索引，重复收到同一消息不会新增索引
func (s *MessageStore) Save(scope MessageScope, msg *message.Message, receivedAt int64) error {
	if s == nil {
		return ErrStoreClosed
	}
	prefix, err := scope.messagePrefix()
	if err != nil {
		return err
	}
	if msg == nil || !validIdentifier(msg.Id) {
		return fmt.Errorf("%w: 消息 ID 不能为空或过长", ErrInvalid)
	}
	if msg.Channel != nil && msg.Channel.Id != scope.ChannelID {
		return fmt.Errorf("%w: 消息所属会话不一致", ErrInvalid)
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(data) > maxMessageBytes {
		return fmt.Errorf("%w: 单条缓存超过 32 MiB 上限", ErrInvalid)
	}
	timestamp := msg.CreateAt
	if timestamp <= 0 {
		timestamp = receivedAt
	}
	if timestamp <= 0 {
		timestamp = time.Now().UnixMilli()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrStoreClosed
	}
	key := []byte("message:" + prefix + encodeIdentifier(msg.Id))
	previous, err := s.db.Get(key, nil)
	if err == nil {
		record, _, decodeErr := decodeMessageRecord(previous)
		if decodeErr != nil {
			return decodeErr
		}
		// 固定首次索引位置，避免重复投递让消息在分页中来回移动。
		timestamp = record.Timestamp
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	record := messageRecord{SchemaVersion: messageSchemaVersion, Timestamp: timestamp, Message: data}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	batch := new(leveldb.Batch)
	batch.Put(key, encoded)
	batch.Put([]byte("time:"+prefix+messagePosition(timestamp, msg.Id)), []byte(msg.Id))
	return s.db.Write(batch, nil)
}

// Get 获取指定账号和会话内的消息
func (s *MessageStore) Get(scope MessageScope, messageID string) (*message.Message, error) {
	if s == nil {
		return nil, ErrStoreClosed
	}
	prefix, err := scope.messagePrefix()
	if err != nil {
		return nil, err
	}
	if !validIdentifier(messageID) {
		return nil, fmt.Errorf("%w: 消息 ID 无效", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrStoreClosed
	}
	data, err := s.db.Get([]byte("message:"+prefix+encodeIdentifier(messageID)), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_, msg, err := decodeMessageRecord(data)
	if err == nil && (msg.Id != messageID || (msg.Channel != nil && msg.Channel.Id != scope.ChannelID)) {
		return nil, ErrCorrupt
	}
	return msg, err
}

// Delete 同时删除消息和对应时间索引
func (s *MessageStore) Delete(scope MessageScope, messageID string) error {
	if s == nil {
		return ErrStoreClosed
	}
	prefix, err := scope.messagePrefix()
	if err != nil {
		return err
	}
	if !validIdentifier(messageID) {
		return fmt.Errorf("%w: 消息 ID 无效", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrStoreClosed
	}
	key := []byte("message:" + prefix + encodeIdentifier(messageID))
	data, err := s.db.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	record, _, err := decodeMessageRecord(data)
	if err != nil {
		return err
	}
	batch := new(leveldb.Batch)
	batch.Delete(key)
	batch.Delete([]byte("time:" + prefix + messagePosition(record.Timestamp, messageID)))
	return s.db.Write(batch, nil)
}

func (scope MessageScope) ownerPrefix() (string, error) {
	if !validIdentifier(scope.AppID) || !validIdentifier(scope.Platform) || !validIdentifier(scope.SelfID) {
		return "", fmt.Errorf("%w: 账号身份不完整", ErrInvalid)
	}
	return encodeIdentifier(scope.AppID) + ":" + encodeIdentifier(scope.Platform) + ":" + encodeIdentifier(scope.SelfID) + ":", nil
}

func (scope MessageScope) messagePrefix() (string, error) {
	owner, err := scope.ownerPrefix()
	if err != nil {
		return "", err
	}
	if !validIdentifier(scope.ChannelID) {
		return "", fmt.Errorf("%w: 会话 ID 无效", ErrInvalid)
	}
	return owner + encodeIdentifier(scope.ChannelID) + ":", nil
}

func validIdentifier(value string) bool {
	return value != "" && len(value) <= 1024
}

func encodeIdentifier(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func messagePosition(timestamp int64, messageID string) string {
	return fmt.Sprintf("%020d:%s", timestamp, encodeIdentifier(messageID))
}

func decodeMessageRecord(data []byte) (*messageRecord, *message.Message, error) {
	var record messageRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, nil, errors.Join(ErrCorrupt, err)
	}
	if record.SchemaVersion != messageSchemaVersion || record.Timestamp <= 0 || len(record.Message) == 0 {
		return nil, nil, ErrCorrupt
	}
	var msg message.Message
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return nil, nil, errors.Join(ErrCorrupt, err)
	}
	if !validIdentifier(msg.Id) {
		return nil, nil, ErrCorrupt
	}
	return &record, &msg, nil
}
