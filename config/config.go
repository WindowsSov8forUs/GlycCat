package config

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/AlecAivazis/survey/v2"
	"gopkg.in/yaml.v3"

	"github.com/WindowsSov8forUs/glyccat/log"
)

var (
	instance *Config
	mutex    sync.Mutex
)

// Config 配置
type Config struct {
	LogLevel   log.LogLevel `yaml:"log_level"`   // 日志等级
	Account    Account      `yaml:"account"`     // QQ 机器人账号配置
	FileServer FileServer   `yaml:"file_server"` // 本地文件服务器配置
	Database   Database     `yaml:"database"`    // 数据库配置
	Satori     Satori       `yaml:"satori"`      // Satori 配置
}

// Account QQ 机器人账号配置
type Account struct {
	BotID     uint64    `yaml:"bot_id"`     // 机器人 QQ 号
	AppID     uint64    `yaml:"app_id"`     // 机器人 ID
	Token     string    `yaml:"token"`      // 机器人令牌
	AppSecret string    `yaml:"app_secret"` // 机器人密钥
	Sandbox   bool      `yaml:"sandbox"`    // 是否使用沙箱环境
	WebSocket WebSocket `yaml:"websocket"`  // WebSocket 配置
	WebHook   QQWebHook `yaml:"webhook"`    // WebHook 配置
}

// WebSocket QQ 机器人 WebSocket 配置
type WebSocket struct {
	Enable     bool     `yaml:"enable"`                // 是否启用 WebSocket
	Shards     uint32   `yaml:"shards"`                // 兼容旧分片配置
	ShardID    *uint32  `yaml:"shard_id,omitempty"`    // 手动分片编号
	ShardCount uint32   `yaml:"shard_count,omitempty"` // 手动分片总数
	Intents    []string `yaml:"intents"`               // 事件订阅
}

// QQWebHook QQ 机器人 WebHook 回调配置
type QQWebHook struct {
	Enable bool   `yaml:"enable"` // 是否启用 WebHook
	Host   string `yaml:"host"`   // WebHook 地址
	Port   uint16 `yaml:"port"`   // WebHook 端口
	Path   string `yaml:"path"`   // WebHook 路径
}

// FileServer 本地文件服务器配置
type FileServer struct {
	Enable      bool   `yaml:"enable"`       // 是否启用对外本地文件服务器
	ExternalURL string `yaml:"external_url"` // 本地文件服务器公网地址 {{ .Host }}:{{ .Port }}
	TTL         uint64 `yaml:"ttl"`          // 文件存储时间，单位秒
}

// Database 数据库配置
type Database struct {
	MessageDatabase MessageDatabase `yaml:"message_database"` // 消息数据库配置
}

// MessageDatabase 消息数据库配置
type MessageDatabase struct {
	Enable bool `yaml:"enable"` // 是否启用消息数据库
	Limit  int  `yaml:"limit"`  // 消息获取数量限制
}

// Satori Satori 配置
type Satori struct {
	Version uint8   `yaml:"version"` // Satori 版本，目前只有 1
	Path    string  `yaml:"path"`    // Satori 部署路径，可以为空
	Token   string  `yaml:"token"`   // 鉴权令牌
	Server  Server  `yaml:"server"`  // 服务器配置
	WebHook WebHook `yaml:"webhook"` // WebHook 客户端配置
}

// Server 服务器配置
type Server struct {
	Host string `yaml:"host"` // 服务器监听地址
	Port uint16 `yaml:"port"` // 服务器端口
}

// WebHook WebHook 客户端配置
type WebHook struct {
	Timeout uint32 `yaml:"timeout"` // 超时时间
}

// GetSatoriToken 获取 Satori 鉴权令牌
func GetSatoriToken() string {
	mutex.Lock()
	defer mutex.Unlock()
	if instance == nil {
		return ""
	}
	return instance.Satori.Token
}

// DefaultConfig 获取默认配置
func DefaultConfig() *Config {
	return &Config{
		LogLevel: log.INFO,
		Account:  Account{WebHook: QQWebHook{Enable: true, Path: "/qqbot"}},
		Database: Database{
			MessageDatabase: MessageDatabase{
				Enable: true,
				Limit:  50, // 默认消息获取数量限制
			},
		},
		Satori: Satori{
			Version: 1,
			Server:  Server{Host: "127.0.0.1", Port: 5140},
			WebHook: WebHook{
				Timeout: 10, // 默认 WebHook 超时时间为 10 秒
			},
		},
	}
}

// DefaultConfigTemplate 默认配置的 YAML 模板
func DefaultConfigTemplate() string {
	defaultConfig := DefaultConfig()

	return DumpConfig(defaultConfig)
}

// DumpConfig 将配置转换为 YAML 字符串
func DumpConfig(conf *Config) string {
	data, err := marshalConfig(conf)
	if err != nil {
		log.Errorf("导出配置失败: %v", err)
		return ""
	}
	return string(data)
}

// marshalConfig 使用 YAML 编码器保存值，并沿用模板中的注释
func marshalConfig(conf *Config) ([]byte, error) {
	if conf == nil {
		return nil, fmt.Errorf("配置不能为空")
	}
	var values, template yaml.Node
	if err := values.Encode(conf); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal([]byte(ConfigTemplate), &template); err != nil {
		return nil, fmt.Errorf("解析配置模板失败: %w", err)
	}
	if len(template.Content) > 0 {
		copyConfigComments(&values, template.Content[0])
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&values); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// copyConfigComments 只复制注释，不改变原始配置值
func copyConfigComments(target, source *yaml.Node) {
	target.HeadComment = source.HeadComment
	target.LineComment = source.LineComment
	target.FootComment = source.FootComment
	if target.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(target.Content); i += 2 {
		for j := 0; j+1 < len(source.Content); j += 2 {
			if target.Content[i].Value == source.Content[j].Value {
				copyConfigComments(target.Content[i], source.Content[j])
				copyConfigComments(target.Content[i+1], source.Content[j+1])
				break
			}
		}
	}
}

// SetConfigByInput 通过用户输入设置配置
func SetConfigByInput(conf *Config) error {
	if err := promptAccountConfig(conf); err != nil {
		return fmt.Errorf("设置账号配置时出错: %w", err)
	}

	if err := promptSatoriConfig(conf); err != nil {
		return fmt.Errorf("设置 Satori 配置时出错: %w", err)
	}

	return nil
}

// promptAccountConfig 提示用户输入账号配置
func promptAccountConfig(conf *Config) error {
	questions := []*survey.Question{
		{
			Name:   "app_id",
			Prompt: &survey.Input{Message: "AppID(机器人 ID):", Help: "通过 QQ 开放平台-管理-开发设置获取 AppID"},
			Validate: func(value interface{}) error {
				text, ok := value.(string)
				if !ok {
					return fmt.Errorf("请输入有效的 AppID")
				}
				id, err := strconv.ParseUint(text, 10, 64)
				if err != nil || id == 0 {
					return fmt.Errorf("AppID 必须为大于 0 的数字")
				}
				return nil
			},
		},
		{
			Name:     "app_secret",
			Prompt:   &survey.Password{Message: "AppSecret(机器人密钥):"},
			Validate: survey.Required,
		},
	}
	answer := struct {
		AppID     uint64 `survey:"app_id"`
		AppSecret string `survey:"app_secret"`
	}{}
	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}
	conf.Account.AppID = answer.AppID
	conf.Account.AppSecret = answer.AppSecret
	connectPrompt := &survey.Select{
		Message: "选择开放平台连接方式:",
		Options: []string{"WebSocket", "WebHook"},
		Default: "WebHook",
		Help:    "请根据机器人实际开放的接收方式选择",
	}
	var mode string
	if err := survey.AskOne(connectPrompt, &mode); err != nil {
		return err
	}
	conf.Account.WebSocket.Enable = mode == "WebSocket"
	conf.Account.WebHook.Enable = mode == "WebHook"
	if conf.Account.WebSocket.Enable {
		return promptAccountWebSocketConfig(conf)
	}
	return promptAccountWebHookConfig(conf)
}

// promptAccountWebSocketConfig 提示用户输入开放平台 WebSocket 配置
func promptAccountWebSocketConfig(conf *Config) error {
	questions := []*survey.Question{
		{
			Name: "shards",
			Prompt: &survey.Input{
				Message: "分片数(Shards):",
				Help:    "建议保持默认的 1 ，多了不知道会发生什么",
				Default: "1",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if shards, err := strconv.ParseUint(str, 10, 32); err != nil || shards < 1 {
						return fmt.Errorf("无效的分片数，请输入一个大于等于 1 的数字")
					}
				}
				return nil
			},
		},
		{
			Name: "intents",
			Prompt: &survey.MultiSelect{
				Message: "请选择需要订阅的事件类型:",
				Options: []string{
					"GUILDS",                  // 频道事件
					"GUILD_MEMBERS",           // 成员事件
					"GUILD_MESSAGES",          // 私域频道消息事件
					"GUILD_MESSAGE_REACTIONS", // 私域频道消息反应事件
					"DIRECT_MESSAGE",          // 频道私信事件
					"GROUP_AND_C2C_EVENT",     // 单聊/群聊消息事件
					"INTERACTION",             // 互动事件
					"MESSAGE_AUDIT",           // 消息审核事件
					"FORUMS_EVENT",            // 私域论坛事件
					"AUDIO_ACTION",            // 音频机器人事件
					"PUBLIC_GUILD_MESSAGES",   // 公域频道消息事件
				},
				Default: []string{"GUILDS", "GUILD_MEMBERS", "PUBLIC_GUILD_MESSAGES"},
				Help:    "使用空格键选择/取消选择，回车键确认",
			},
			Validate: func(val interface{}) error {
				if selected, ok := val.([]string); ok {
					if len(selected) == 0 {
						return fmt.Errorf("至少选择一个事件类型")
					}
				}
				return nil
			},
		},
	}

	answer := struct {
		Shards  uint32   `survey:"shards"`
		Intents []string `survey:"intents"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	conf.Account.WebSocket.Shards = answer.Shards
	conf.Account.WebSocket.Intents = answer.Intents

	return nil
}

// promptAccountWebHookConfig 提示用户输入开放平台 WebHook 配置
func promptAccountWebHookConfig(conf *Config) error {
	questions := []*survey.Question{
		{
			Name: "host",
			Prompt: &survey.Input{
				Message: "监听 QQ 开放平台回调信息地址:",
				Default: "0.0.0.0",
				Help:    "监听地址，默认监听所有 IP 地址",
			},
		},
		{
			Name: "port",
			Prompt: &survey.Input{
				Message: "监听端口:",
				Default: "8081",
				Help:    "本地 HTTP 监听端口，公网 HTTPS 和端口转发由反向代理提供",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if port, err := strconv.ParseUint(str, 10, 16); err != nil || port < 1 || port > 65535 {
						return fmt.Errorf("无效的端口号，请输入一个有效的端口号")
					}
				}
				return nil
			},
		},
		{
			Name: "path",
			Prompt: &survey.Input{
				Message: "WebHook 路径:",
				Default: "",
				Help:    "WebHook 回调路径，留空使用 /qqbot",
			},
		},
	}

	answer := struct {
		Host string `survey:"host"`
		Port uint16 `survey:"port"`
		Path string `survey:"path"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	// 修正 path 的格式
	if answer.Path != "" && !strings.HasPrefix(answer.Path, "/") {
		answer.Path = "/" + answer.Path
	}

	conf.Account.WebHook.Host = answer.Host
	conf.Account.WebHook.Port = answer.Port
	conf.Account.WebHook.Path = answer.Path

	return nil
}

// promptSatoriConfig 提示用户输入 Satori 配置
func promptSatoriConfig(conf *Config) error {
	questions := []*survey.Question{
		{
			Name: "version",
			Prompt: &survey.Input{
				Message: "Satori 版本:",
				Default: "1",
				Help:    "使用的 Satori 协议版本(仅数字)，目前只存在 v1",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if version, err := strconv.ParseUint(str, 10, 8); err != nil {
						return fmt.Errorf("无效的 Satori 版本，请输入一个大于等于 1 的数字")
					} else if version != 1 {
						return fmt.Errorf("目前只支持 Satori v1 版本")
					}
				}
				return nil
			},
		},
		{
			Name: "host",
			Prompt: &survey.Input{
				Message: "Satori 服务器监听地址:",
				Default: "127.0.0.1",
				Help:    "Satori 服务器监听的 IP 地址",
			},
		},
		{
			Name: "port",
			Prompt: &survey.Input{
				Message: "Satori 服务器端口:",
				Default: "5140",
				Help:    "Satori 服务器所在的端口",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if port, err := strconv.ParseUint(str, 10, 16); err != nil || port < 1 || port > 65535 {
						return fmt.Errorf("无效的端口号，请输入一个有效的端口号")
					}
				}
				return nil
			},
		},
		{
			Name: "path",
			Prompt: &survey.Input{
				Message: "Satori 服务器路径:",
				Default: "",
				Help:    "Satori 服务器所在的路径，可以为空",
			},
		},
		{
			Name: "token",
			Prompt: &survey.Input{
				Message: "Satori 服务器令牌:",
				Default: "",
				Help:    "用于验证 Satori 服务器的令牌，如果不设置则不会进行鉴权",
			},
		},
	}

	answer := struct {
		Version uint8  `survey:"version"`
		Host    string `survey:"host"`
		Port    uint16 `survey:"port"`
		Path    string `survey:"path"`
		Token   string `survey:"token"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	conf.Satori.Version = answer.Version
	conf.Satori.Path = answer.Path
	conf.Satori.Token = answer.Token
	conf.Satori.Server.Host = answer.Host
	conf.Satori.Server.Port = answer.Port

	return nil
}

// IsFileServerEnabled 是否启用本地文件服务器
func IsFileServerEnabled() bool {
	mutex.Lock()
	defer mutex.Unlock()

	if instance == nil {
		log.Warn("配置未加载，无法判断是否启用本地文件服务器。")
		return false
	}
	return instance.FileServer.Enable
}

// GetFileServerURL 获取本地文件服务器地址
func GetFileServerURL() string {
	mutex.Lock()
	defer mutex.Unlock()

	if instance == nil {
		log.Warn("配置未加载，无法获取本地文件服务器地址。")
		return ""
	}
	return instance.FileServer.ExternalURL
}
