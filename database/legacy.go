package database

import (
	"bytes"
	"context"
	"encoding/gob"
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
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return result, err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(targetPath); resolveErr == nil {
		targetPath = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return result, resolveErr
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
	// 同步最后一个写入屏障，确保正常完成时前面的迁移写入已落盘。
	target.mu.Lock()
	err = target.db.Put([]byte("migration_completed"), []byte(strconv.Itoa(result.Read)), &opt.WriteOptions{Sync: true})
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
