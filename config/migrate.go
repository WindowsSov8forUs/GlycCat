package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/WindowsSov8forUs/glyccat/log"
	"gopkg.in/yaml.v3"
)

// prepareConfig 只在加载边界转换无版本的旧配置，运行时不再解释旧字段。
func prepareConfig(data []byte) (*Config, bool, error) {
	fields, err := configDocument(data)
	if err != nil {
		return nil, false, err
	}
	if _, present := fields["config_version"]; present {
		conf, err := decodeConfig(data)
		return conf, false, err
	}

	// 旧字段仅用于一次性迁移；未知字段或错误类型仍然报错，不能因删除字段而掩盖。
	var legacy struct {
		LogLevel  log.LogLevel `yaml:"log_level"`
		DebugMode bool         `yaml:"debug_mode"`
		Account   struct {
			BotID     uint64 `yaml:"bot_id"`
			AppID     uint64 `yaml:"app_id"`
			Token     string `yaml:"token"`
			AppSecret string `yaml:"app_secret"`
			Sandbox   bool   `yaml:"sandbox"`
			WebSocket struct {
				WebSocket `yaml:",inline"`
				Shards    uint32 `yaml:"shards"`
			} `yaml:"websocket"`
			WebHook QQWebHook `yaml:"webhook"`
		} `yaml:"account"`
		FileServer struct {
			Enable      bool   `yaml:"enable"`
			ExternalURL string `yaml:"external_url"`
			TTL         uint64 `yaml:"ttl"`
		} `yaml:"file_server"`
		Database Database `yaml:"database"`
		Satori   Satori   `yaml:"satori"`
	}
	conf := DefaultConfig()
	legacy.LogLevel = conf.LogLevel
	legacy.Account.WebHook = conf.Account.WebHook
	legacy.Database = conf.Database
	legacy.Satori = conf.Satori
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&legacy); err != nil {
		return nil, false, fmt.Errorf("解析旧配置时出错: %w", err)
	}
	conf.LogLevel = legacy.LogLevel
	conf.Account = Account{
		AppID: legacy.Account.AppID, AppSecret: legacy.Account.AppSecret,
		Sandbox: legacy.Account.Sandbox, WebSocket: legacy.Account.WebSocket.WebSocket,
		WebHook: legacy.Account.WebHook,
	}
	conf.Database = legacy.Database
	conf.Satori = legacy.Satori
	if conf.Account.WebSocket.Enable {
		// 旧 WebSocket 配置可能没有 WebHook 段，迁移时明确写入互斥开关。
		if !hasConfigField(fields, "account", "webhook", "enable") {
			conf.Account.WebHook.Enable = false
		}
		if len(conf.Account.WebSocket.Intents) == 0 {
			// 仅展开旧配置原本采用的六项订阅，不推测权限或改变非空列表。
			conf.Account.WebSocket.Intents = []string{"GUILDS", "GUILD_MEMBERS", "PUBLIC_GUILD_MESSAGES", "GROUP_AND_C2C_EVENT", "INTERACTION", "MESSAGE_AUDIT"}
		}
	}
	if err := validateStoredConfig(conf); err != nil {
		return nil, false, err
	}
	return conf, true, nil
}

// configDocument 检查文档形状及重复键；这里只读取字段出现性，不重新编码用户值。
func configDocument(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("配置文件为空")
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("配置文件超过 1 MiB 上限")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("解析配置文件时出错: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("配置文件只能包含一个 YAML 文档")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("配置必须为 YAML 映射")
	}
	var fields map[string]any
	if err := document.Decode(&fields); err != nil {
		return nil, fmt.Errorf("解析配置文件时出错: %w", err)
	}
	return fields, nil
}

func hasConfigField(fields map[string]any, keys ...string) bool {
	for i, key := range keys {
		value, present := fields[key]
		if !present {
			return false
		}
		if i == len(keys)-1 {
			return true
		}
		fields, _ = value.(map[string]any)
	}
	return true
}
