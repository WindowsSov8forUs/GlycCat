package database

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// MigrationResult 迁移统计，不在报告中输出消息正文或账号密钥
type MigrationResult struct {
	Read     int `json:"read"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

// MigrateMessages 从停机备份只读迁移旧消息库，账号归属由调用者显式确认
// 已存在的新记录不覆盖；原库不删除，迁移中断后可以重复执行。
func MigrateMessages(ctx context.Context, sourcePath, targetPath, appID, selfID string) (result MigrationResult, resultErr error) {
	id, err := strconv.ParseUint(appID, 10, 64)
	if err != nil || id == 0 || !validIdentifier(selfID) {
		return result, fmt.Errorf("必须明确指定旧库归属的 AppID 和新版 self_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return result, err
	}
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return result, fmt.Errorf("找不到旧库备份目录: %w", err)
	}
	targetPath, err = resolveMigrationTarget(targetPath)
	if err != nil {
		return result, err
	}
	if strings.EqualFold(sourcePath, targetPath) || strings.HasPrefix(strings.ToLower(targetPath), strings.ToLower(sourcePath)+string(os.PathSeparator)) {
		return result, fmt.Errorf("新库不能是原库或原库内的子目录")
	}
	source, err := leveldb.OpenFile(sourcePath, &opt.Options{ReadOnly: true, ErrorIfMissing: true})
	if err != nil {
		return result, fmt.Errorf("只读打开旧库失败，请先停止程序并备份数据库: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	target, err := OpenMessageStore(targetPath, 0)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, target.Close()) }()
	iter := source.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Read++
		if len(iter.Value()) > maxMessageBytes {
			result.Failed++
			continue
		}
		scope, msg, err := decodeLegacyMessage(string(iter.Key()), iter.Value(), strconv.FormatUint(id, 10), selfID)
		if err != nil {
			result.Failed++
			continue
		}
		if _, err := target.Get(scope, msg.Id); err == nil {
			result.Skipped++
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return result, fmt.Errorf("检查新库时失败，已处理 %d 条: %w", result.Read, err)
		}
		if err := target.Save(scope, msg, msg.CreateAt); err != nil {
			return result, fmt.Errorf("写入新库失败，已处理 %d 条: %w", result.Read, err)
		}
		result.Imported++
	}
	if err := iter.Error(); err != nil {
		return result, err
	}
	// 同步本次处理记录；存在失败项时不能写成迁移全部完成。
	owner, _ := (MessageScope{AppID: strconv.FormatUint(id, 10), Platform: "qq", SelfID: selfID}).ownerPrefix()
	progress, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	target.mu.Lock()
	err = target.db.Put([]byte("migration_progress:"+owner), progress, &opt.WriteOptions{Sync: true})
	target.mu.Unlock()
	if err != nil {
		return result, err
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("%d 条旧记录格式不完整，未猜测或导入；原始备份保持不变", result.Failed)
	}
	return result, nil
}

func decodeLegacyMessage(key string, data []byte, appID, selfID string) (MessageScope, *message.Message, error) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || (parts[0] != "group" && parts[0] != "private") || !validIdentifier(parts[1]) || !validIdentifier(parts[2]) {
		return MessageScope{}, nil, ErrCorrupt
	}
	// gob 仅用于读取旧库的导出字段；新库仍保存版本化 JSON，不沿用旧格式写入。
	var msg message.Message
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&msg); err != nil {
		return MessageScope{}, nil, err
	}
	if msg.Id != parts[2] || msg.CreateAt <= 0 || msg.User == nil || msg.User.Id == "" {
		return MessageScope{}, nil, ErrCorrupt
	}
	channelID := parts[1]
	kind := channel.ChannelTypeText
	if parts[0] == "private" {
		// 旧键已经明确标记私聊才补前缀，不从裸 OpenID 猜测会话类型。
		channelID = "private:" + channelID
		kind = channel.ChannelTypeDirect
		msg.Guild = nil
	} else if msg.Guild == nil {
		msg.Guild = &guild.Guild{Id: channelID}
	}
	if msg.Channel != nil && msg.Channel.Id != parts[1] && msg.Channel.Id != channelID {
		return MessageScope{}, nil, ErrCorrupt
	}
	msg.Channel = &channel.Channel{Id: channelID, Type: kind}
	return MessageScope{AppID: appID, Platform: "qq", SelfID: selfID, ChannelID: channelID}, &msg, nil
}

// resolveMigrationTarget 同时解析尚未创建目标的父目录链接，避免将新库写回原库内
func resolveMigrationTarget(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing := absolute
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("迁移目标没有可用的父目录: %w", err)
		}
		// 已存在但无法解析的链接不能当成待创建目录。
		if _, statErr := os.Lstat(existing); statErr == nil {
			return "", fmt.Errorf("迁移目标包含失效链接: %w", err)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}
}
