package config

import (
	"fmt"
	"net"
	"path"
	"strings"

	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

// NormalizeAndValidate 统一文件和交互配置的默认值与校验规则
func (conf *Config) NormalizeAndValidate() error {
	if conf == nil {
		return fmt.Errorf("配置不能为空")
	}
	if conf.LogLevel < log.OFF || conf.LogLevel > log.ALL {
		return fmt.Errorf("日志等级必须在 0 到 7 之间")
	}
	if conf.Account.AppID == 0 || strings.TrimSpace(conf.Account.AppSecret) == "" {
		return fmt.Errorf("请填写有效的 AppID 和 AppSecret，新版 SDK 不需要旧机器人 Token")
	}
	if conf.Account.WebSocket.Enable == conf.Account.WebHook.Enable {
		return fmt.Errorf("WebSocket 和 WebHook 必须且只能启用一个")
	}
	if conf.Database.MessageDatabase.Limit < 0 {
		return fmt.Errorf("消息查询数量限制不能为负数")
	}
	if conf.Satori.Version == 0 {
		conf.Satori.Version = 1
	}
	if conf.Satori.Version != 1 {
		return fmt.Errorf("目前只支持 Satori v1 协议")
	}
	if conf.Satori.Server.Port == 0 {
		conf.Satori.Server.Port = protocol.DefaultAPIPort
	}
	var err error
	conf.Satori.Server.Host, err = normalizeHost(conf.Satori.Server.Host, "127.0.0.1")
	if err != nil {
		return fmt.Errorf("Satori 监听地址无效: %w", err)
	}
	conf.Satori.Path, err = normalizePath(conf.Satori.Path, "")
	if err != nil {
		return fmt.Errorf("Satori 部署路径无效: %w", err)
	}
	if conf.Satori.Path == "/" {
		conf.Satori.Path = ""
	}
	if strings.ContainsAny(conf.Satori.Token, "\r\n\x00") {
		return fmt.Errorf("Satori 鉴权令牌不能包含换行或空字符")
	}
	ip := net.ParseIP(conf.Satori.Server.Host)
	loopback := conf.Satori.Server.Host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && strings.TrimSpace(conf.Satori.Token) == "" {
		return fmt.Errorf("非本机监听必须设置独立的 Satori 鉴权令牌")
	}
	if conf.Account.WebHook.Enable {
		conf.Account.WebHook.Host, err = normalizeHost(conf.Account.WebHook.Host, conf.Satori.Server.Host)
		if err != nil {
			return fmt.Errorf("QQ 回调监听地址无效: %w", err)
		}
		if conf.Account.WebHook.Port == 0 {
			conf.Account.WebHook.Port = conf.Satori.Server.Port
		}
		conf.Account.WebHook.Path, err = normalizePath(conf.Account.WebHook.Path, "/qqbot")
		if err != nil {
			return fmt.Errorf("QQ 回调路径无效: %w", err)
		}
		base := conf.Satori.Path + "/v1"
		if conf.Account.WebHook.Path == base || strings.HasPrefix(conf.Account.WebHook.Path, base+"/") {
			return fmt.Errorf("QQ 回调路径不能占用 Satori 协议路径")
		}
		a, b := conf.Account.WebHook.Host, conf.Satori.Server.Host
		if conf.Account.WebHook.Port == conf.Satori.Server.Port && a != b && listenHostsOverlap(a, b) {
			return fmt.Errorf("QQ 回调和 Satori 监听地址重叠，请使用完全相同的地址共享监听，或配置不同端口")
		}
	}
	if conf.Account.WebSocket.Enable {
		ws := &conf.Account.WebSocket
		if ws.Shards != 0 {
			log.Warn("检测到旧的 WebSocket 分片配置，将使用自动分片。手动指定分片时，请同时配置 shard_id 和 shard_count。")
			ws.Shards = 0
		}
		if (ws.ShardID == nil) != (ws.ShardCount == 0) {
			return fmt.Errorf("手动分片必须同时填写 shard_id 和大于 0 的 shard_count")
		}
		if ws.ShardID != nil && *ws.ShardID >= ws.ShardCount {
			return fmt.Errorf("shard_id 必须小于 shard_count")
		}
		for i, value := range ws.Intents {
			name := strings.ToUpper(strings.TrimSpace(value))
			if !knownIntents[name] {
				return fmt.Errorf("存在不支持的 WebSocket 事件订阅，请核对 intents 名称")
			}
			ws.Intents[i] = name
		}
	}
	return nil
}

func normalizeHost(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), nil
	}
	if value == "" || strings.ContainsAny(value, ":/\\?#[] \t\r\n\x00") {
		return "", fmt.Errorf("应填写主机名或 IP，不应包含端口或 URL")
	}
	return strings.ToLower(value), nil
}

func normalizePath(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if strings.ContainsAny(value, "?#{*}\\%\r\n\x00") {
		return "", fmt.Errorf("路径不能包含查询、通配符或转义字符")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("路径不能包含相对目录")
		}
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/")), nil
}

func listenHostsOverlap(a, b string) bool {
	if a == "0.0.0.0" || a == "::" || b == "0.0.0.0" || b == "::" {
		return true
	}
	isLoopback := func(host string) bool {
		ip := net.ParseIP(host)
		return host == "localhost" || (ip != nil && ip.IsLoopback())
	}
	return isLoopback(a) && isLoopback(b) && (a == "localhost" || b == "localhost")
}

var knownIntents = map[string]bool{
	"GUILDS": true, "GUILD_MEMBERS": true, "GUILD_MESSAGES": true,
	"GUILD_MESSAGE_REACTIONS": true, "GUILD_MESSAGE_REACTION": true,
	"DIRECT_MESSAGE": true, "DIRECT_MESSAGES": true,
	"GROUP_AND_C2C_EVENT": true, "C2C_GROUP_AT_MESSAGES": true, "USER_MESSAGES": true,
	"INTERACTION": true, "MESSAGE_AUDIT": true,
	"FORUM_EVENT": true, "FORUMS_EVENT": true, "OPEN_FORUM_EVENT": true, "OPEN_FORUMS_EVENT": true,
	"AUDIO_ACTION": true, "AUDIO_LIVE_MEMBER": true, "AUDIO_OR_LIVE_CHANNEL_MEMBER": true,
	"AT_MESSAGES": true, "PUBLIC_GUILD_MESSAGES": true,
}
