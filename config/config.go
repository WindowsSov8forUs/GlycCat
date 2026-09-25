package config

import (
	"bytes"
	"fmt"
	"os"
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
	Enable  bool     `yaml:"enable"`  // 是否启用 WebSocket
	Shards  uint32   `yaml:"shards"`  // 分片数
	Intents []string `yaml:"intents"` // 事件订阅
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
	return instance.Satori.Token
}

// DefaultConfig 获取默认配置
func DefaultConfig() *Config {
	return &Config{
		LogLevel: log.INFO,
		Database: Database{
			MessageDatabase: MessageDatabase{
				Enable: true,
				Limit:  50, // 默认消息获取数量限制
			},
		},
		Satori: Satori{
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

	if err := promptFileServerConfig(conf); err != nil {
		return fmt.Errorf("设置文件服务器配置时出错: %w", err)
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
			Name: "bot_id",
			Prompt: &survey.Input{
				Message: "机器人 QQ 号:",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的机器人 QQ 号",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if _, err := strconv.ParseUint(str, 10, 64); err != nil {
						return fmt.Errorf("无效的机器人 QQ 号，请输入一个有效的数字")
					}
				}
				return nil
			},
		},
		{
			Name: "app_id",
			Prompt: &survey.Input{
				Message: "AppID(机器人 ID ):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 AppID",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if _, err := strconv.ParseUint(str, 10, 64); err != nil {
						return fmt.Errorf("无效的 AppID，请输入一个有效的数字")
					}
				}
				return nil
			},
		},
		{
			Name: "token",
			Prompt: &survey.Input{
				Message: "Token(机器人令牌):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 Token",
			},
			Validate: survey.Required,
		},
		{
			Name: "app_secret",
			Prompt: &survey.Password{
				Message: "AppSecret(机器人密钥):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 AppSecret",
			},
			Validate: survey.Required,
		},
	}

	answer := struct {
		BotID     uint64 `survey:"bot_id"`
		AppID     uint64 `survey:"app_id"`
		Token     string `survey:"token"`
		AppSecret string `survey:"app_secret"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	conf.Account.BotID = answer.BotID
	conf.Account.AppID = answer.AppID
	conf.Account.Token = answer.Token
	conf.Account.AppSecret = answer.AppSecret

	// 提问选择 WebHook 还是 WebSocket
	connectPrompt := &survey.Select{
		Message: "选择开放平台连接方式:",
		Options: []string{"WebSocket", "WebHook"},
		Default: "WebHook",
		Help:    "目前 QQ 开放平台已逐渐取消对 WebSocket 的支持，建议使用 WebHook 连接方式",
	}
	connectAnswer := ""
	if err := survey.AskOne(connectPrompt, &connectAnswer); err != nil {
		return err
	}

	// 根据用户选择的连接方式进行配置
	if connectAnswer == "WebSocket" {
		conf.Account.WebSocket.Enable = true
		conf.Account.WebHook.Enable = false
		if err := promptAccountWebSocketConfig(conf); err != nil {
			return err
		}
	} else {
		conf.Account.WebSocket.Enable = false
		conf.Account.WebHook.Enable = true
		if err := promptAccountWebHookConfig(conf); err != nil {
			return err
		}
	}

	return nil
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
				Default: "443",
				Help:    "监听端口，目前开放平台仅支持 80、443、8080、8443 四个端口",
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
				Help:    "WebHook 回调的路径，默认为空，即根路径",
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

// promptFileServerConfig 提示用户输入本地文件服务器配置
func promptFileServerConfig(conf *Config) error {
	enablePrompt := &survey.Confirm{
		Message: "是否启用本地文件服务器?",
		Default: true,
		Help:    "启用后可以通过本地文件服务器上传和下载文件，否则可能无法发送富媒体消息。默认启用",
	}
	var enable bool
	if err := survey.AskOne(enablePrompt, &enable); err != nil {
		return err
	}

	if enable {
		conf.FileServer.Enable = true
	} else {
		return nil
	}

	questions := []*survey.Question{
		{
			Name: "external_url",
			Prompt: &survey.Input{
				Message: "公网地址:",
				Help:    "用于访问本地文件服务器的公网地址",
			},
		},
		{
			Name: "ttl",
			Prompt: &survey.Input{
				Message: "文件有效期(秒):",
				Help:    "用于设置文件的有效期，默认 3600 秒，若为 0 则表示永久有效",
				Default: "3600",
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if _, err := strconv.ParseUint(str, 10, 64); err != nil {
						return fmt.Errorf("无效的文件有效期")
					}
				}
				return nil
			},
		},
	}

	answer := struct {
		ExternalURL string `survey:"external_url"`
		TTL         uint64 `survey:"ttl"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	conf.FileServer.ExternalURL = answer.ExternalURL
	conf.FileServer.TTL = answer.TTL

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
				Default: "8080",
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

// LoadConfig 加载配置
func LoadConfig(path string) (*Config, error) {
	mutex.Lock()
	defer mutex.Unlock()

	var config *Config

	// 检查 config.yml 是否存在
	if _, err := os.Stat("config.yml"); os.IsNotExist(err) {
		config = DefaultConfig()

		fmt.Printf("%s 未检测到配置文件，即将进入首次配置流程\n", log.InfoMark)
		if err := SetConfigByInput(config); err != nil {
			fmt.Printf("%s 获取用户配置项时出错: %v\n", log.FailMark, err)
			return nil, err
		}

		configData := DumpConfig(config)

		// 写入 config.yml
		err = os.WriteFile("config.yml", []byte(configData), 0644)
		if err != nil {
			return nil, fmt.Errorf("%s 写入配置文件时出错: %v", log.FailMark, err)
		}
	} else {
		// 确保配置完整性
		if err := ensureConfigComplete(path); err != nil {
			return nil, err
		}

		// 读取配置文件
		configData, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		// 初始化配置结构体
		config = &Config{}
		if err = yaml.Unmarshal(configData, config); err != nil {
			return nil, err
		}
	}

	instance = config
	return instance, nil
}

// ensureConfigComplete 检查配置是否完整
func ensureConfigComplete(path string) error {
	// 读取当前配置文件
	currentConfigData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 检查配置模板是否可用
	if ConfigTemplate == "" {
		return fmt.Errorf("配置模板不可用")
	}

	// 直接解析 YAML 内容到 map，而不是结构体
	var currentConfigMap map[string]interface{}
	if err = yaml.Unmarshal(currentConfigData, &currentConfigMap); err != nil {
		return fmt.Errorf("解析当前配置文件失败: %w", err)
	}

	// 解析模板配置到 map
	var templateConfigMap map[string]interface{}
	if err = yaml.Unmarshal([]byte(DefaultConfigTemplate()), &templateConfigMap); err != nil {
		return fmt.Errorf("解析默认配置模板失败: %w", err)
	}

	// 检测缺失的配置项
	missingKeys := findMissingConfigKeysFromMaps(currentConfigMap, templateConfigMap)
	// 检测无效的配置项（使用相同的 map 数据源）
	invalidKeys := findInvalidConfigKeysFromMaps(currentConfigMap, templateConfigMap)

	// 如果没有问题，直接返回
	if len(missingKeys) == 0 && len(invalidKeys) == 0 {
		return nil
	}

	// 显示问题摘要
	displayConfigIssues(missingKeys, invalidKeys)

	// 询问用户是否要自动修复
	shouldFix, err := promptUserForConfigFix()
	if err != nil {
		return fmt.Errorf("获取用户输入失败: %w", err)
	}

	if !shouldFix {
		return fmt.Errorf("配置文件更新流程终止")
	}

	// 执行配置修复
	return fixConfigFile(path, currentConfigData)
}

// findMissingConfigKeysFromMaps 从 map 中查找缺失的配置键
func findMissingConfigKeysFromMaps(current, template map[string]interface{}) []string {
	var missingKeys []string
	findMissingKeysRecursive("", current, template, &missingKeys)
	return missingKeys
}

// findMissingKeysRecursive 递归查找缺失的键
func findMissingKeysRecursive(prefix string, current, template map[string]interface{}, missing *[]string) {
	for key, templateValue := range template {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		currentValue, exists := current[key]
		if !exists {
			*missing = append(*missing, fullKey)
			continue
		}

		// 递归处理嵌套结构
		if templateMap, ok := templateValue.(map[string]interface{}); ok {
			if currentMap, ok := currentValue.(map[string]interface{}); ok {
				findMissingKeysRecursive(fullKey, currentMap, templateMap, missing)
			}
		}
	}
}

// findInvalidConfigKeysFromMaps 从 map 中查找无效的配置键（递归版本）
func findInvalidConfigKeysFromMaps(current, template map[string]interface{}) []string {
	var invalidKeys []string
	findInvalidKeysRecursive("", current, template, &invalidKeys)
	return invalidKeys
}

// findInvalidKeysRecursive 递归查找无效的键
func findInvalidKeysRecursive(prefix string, current, template map[string]interface{}, invalid *[]string) {
	for key, currentValue := range current {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		templateValue, exists := template[key]
		if !exists {
			*invalid = append(*invalid, fullKey)
			continue
		}

		// 递归处理嵌套结构
		if currentMap, ok := currentValue.(map[string]interface{}); ok {
			if templateMap, ok := templateValue.(map[string]interface{}); ok {
				findInvalidKeysRecursive(fullKey, currentMap, templateMap, invalid)
			}
		}
	}
}

// displayConfigIssues 显示配置问题
func displayConfigIssues(missingKeys, invalidKeys []string) {
	fmt.Printf("%s 配置文件检查结果:\n", log.InfoMark)

	if len(missingKeys) > 0 {
		fmt.Printf("  %s 缺失的配置项 (%d个):\n", log.WarningMark, len(missingKeys))
		for _, key := range missingKeys {
			fmt.Printf("    - %s\n", key)
		}
	}

	if len(invalidKeys) > 0 {
		fmt.Printf("  %s 无效的配置项 (%d个):\n", log.WarningMark, len(invalidKeys))
		for _, key := range invalidKeys {
			fmt.Printf("    - %s\n", key)
		}
	}
}

// promptUserForConfigFix 询问用户是否要修复配置文件
func promptUserForConfigFix() (bool, error) {
	prompt := &survey.Confirm{
		Message: "是否进入配置文件更新流程？",
		Default: true,
	}

	var shouldFix bool
	err := survey.AskOne(prompt, &shouldFix)
	return shouldFix, err
}

// fixConfigFile 修复配置文件
func fixConfigFile(configPath string, originalData []byte) error {
	// 创建备份文件名（带时间戳）
	backupPath := "config.yml.backup"

	// 备份原配置文件
	if err := os.WriteFile(backupPath, originalData, 0644); err != nil {
		return fmt.Errorf("备份配置文件失败: %w", err)
	}

	fmt.Printf("%s 原配置文件已备份为: %s\n", log.SuccessMark, backupPath)

	// 尝试合并配置
	mergedConfig, err := mergeConfigWithTemplate(originalData)
	if err != nil {
		// 如果合并失败，使用模板重新生成
		fmt.Printf("%s 配置合并失败，将使用模板重新生成配置文件\n", log.WarningMark)
		return regenerateConfigFromTemplate(configPath)
	}

	var finalConfig *Config
	// 解析合并后的配置以进行交互式配置
	var config Config
	if err := yaml.Unmarshal(mergedConfig, &config); err != nil {
		fmt.Printf("%s 解析合并配置失败，将使用默认配置\n", log.WarningMark)
		finalConfig = DefaultConfig()
	} else {
		// 进行交互式配置
		if err := interactiveConfigUpdate(&config); err != nil {
			fmt.Printf("%s 配置更新流程失败: %v，将使用合并后的配置\n", log.WarningMark, err)
			finalConfig = &config
		} else {
			finalConfig = &config
		}
	}

	// 重新导出配置
	finalConfigData := DumpConfig(finalConfig)
	mergedConfig = []byte(finalConfigData)

	// 写入合并后的配置
	if err := os.WriteFile(configPath, mergedConfig, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	fmt.Printf("%s 配置文件已更新，原配置已备份为: %s\n", log.SuccessMark, backupPath)

	fmt.Print("\n==========================================================\n\n")

	return nil
}

// mergeConfigWithTemplate 将现有配置与模板合并
func mergeConfigWithTemplate(originalData []byte) ([]byte, error) {
	// 只有实际出现的字段才会覆盖默认值，false、0 和空字符串不是字段缺失。
	conf := DefaultConfig()
	if err := yaml.Unmarshal(originalData, conf); err != nil {
		return nil, fmt.Errorf("解析原配置失败: %w", err)
	}
	return marshalConfig(conf)
}

// mergeConfigStructs 合并配置结构体
func mergeConfigStructs(template, original *Config) *Config {
	result := *template // 复制模板配置

	// 合并基本字段（只有非零值才覆盖）
	if original.LogLevel != 0 {
		result.LogLevel = original.LogLevel
	}

	// 合并 Account 配置
	if original.Account.BotID != 0 {
		result.Account.BotID = original.Account.BotID
	}
	if original.Account.AppID != 0 {
		result.Account.AppID = original.Account.AppID
	}
	if original.Account.Token != "" {
		result.Account.Token = original.Account.Token
	}
	if original.Account.AppSecret != "" {
		result.Account.AppSecret = original.Account.AppSecret
	}
	result.Account.Sandbox = original.Account.Sandbox // bool 类型直接覆盖

	// 合并 WebSocket 配置
	result.Account.WebSocket.Enable = original.Account.WebSocket.Enable
	if original.Account.WebSocket.Shards != 0 {
		result.Account.WebSocket.Shards = original.Account.WebSocket.Shards
	}
	if len(original.Account.WebSocket.Intents) > 0 {
		result.Account.WebSocket.Intents = original.Account.WebSocket.Intents
	}

	// 合并 WebHook 配置
	result.Account.WebHook.Enable = original.Account.WebHook.Enable
	if original.Account.WebHook.Host != "" {
		result.Account.WebHook.Host = original.Account.WebHook.Host
	}
	if original.Account.WebHook.Port != 0 {
		result.Account.WebHook.Port = original.Account.WebHook.Port
	}
	if original.Account.WebHook.Path != "" {
		result.Account.WebHook.Path = original.Account.WebHook.Path
	}

	// 合并 FileServer 配置
	result.FileServer.Enable = original.FileServer.Enable
	if original.FileServer.ExternalURL != "" {
		result.FileServer.ExternalURL = original.FileServer.ExternalURL
	}
	if original.FileServer.TTL != 0 {
		result.FileServer.TTL = original.FileServer.TTL
	}

	// 合并 Database 配置
	result.Database.MessageDatabase.Enable = original.Database.MessageDatabase.Enable
	if original.Database.MessageDatabase.Limit != 0 {
		result.Database.MessageDatabase.Limit = original.Database.MessageDatabase.Limit
	}

	// 合并 Satori 配置
	if original.Satori.Version != 0 {
		result.Satori.Version = original.Satori.Version
	}
	if original.Satori.Path != "" {
		result.Satori.Path = original.Satori.Path
	}
	if original.Satori.Token != "" {
		result.Satori.Token = original.Satori.Token
	}
	if original.Satori.Server.Host != "" {
		result.Satori.Server.Host = original.Satori.Server.Host
	}
	if original.Satori.Server.Port != 0 {
		result.Satori.Server.Port = original.Satori.Server.Port
	}
	if original.Satori.WebHook.Timeout != 0 {
		result.Satori.WebHook.Timeout = original.Satori.WebHook.Timeout
	}

	return &result
}

// regenerateConfigFromTemplate 从模板重新生成配置文件
func regenerateConfigFromTemplate(configPath string) error {
	// 使用 DefaultConfigTemplate() 而不是 ConfigTemplate
	if err := os.WriteFile(configPath, []byte(DefaultConfigTemplate()), 0644); err != nil {
		return fmt.Errorf("重新生成配置文件失败: %w", err)
	}

	fmt.Printf("%s 配置文件已从模板重新生成。\n", log.SuccessMark)
	return nil
}

// interactiveConfigUpdate 交互式更新配置
func interactiveConfigUpdate(conf *Config) error {
	fmt.Printf("%s 配置更新选项:\n", log.InfoMark)

	// 检查并配置连接方式
	if needsConnectionConfig(conf) {
		if err := promptConnectionConfig(conf); err != nil {
			return fmt.Errorf("配置连接方式失败: %w", err)
		}
	}

	// 检查并配置账号信息
	if needsAccountConfig(conf) {
		if err := promptAccountBasicConfig(conf); err != nil {
			return fmt.Errorf("配置机器人账号失败: %w", err)
		}
	}

	// 检查并配置文件服务器
	if needsFileServerConfig(conf) {
		if err := promptFileServerConfig(conf); err != nil {
			return fmt.Errorf("配置文件服务器失败: %w", err)
		}
	}

	// 检查并配置 Satori 服务器
	if needsSatoriConfig(conf) {
		if err := promptSatoriConfig(conf); err != nil {
			return fmt.Errorf("配置 Satori 服务器失败: %w", err)
		}
	}

	return nil
}

// needsConnectionConfig 检查是否需要配置连接方式
func needsConnectionConfig(conf *Config) bool {
	return !conf.Account.WebSocket.Enable && !conf.Account.WebHook.Enable
}

// needsAccountConfig 检查是否需要配置账号信息
func needsAccountConfig(conf *Config) bool {
	return conf.Account.BotID == 0 ||
		conf.Account.AppID == 0 ||
		conf.Account.Token == "" ||
		conf.Account.AppSecret == ""
}

// needsFileServerConfig 检查是否需要配置文件服务器
func needsFileServerConfig(conf *Config) bool {
	return conf.FileServer.Enable && conf.FileServer.ExternalURL == ""
}

// needsSatoriConfig 检查是否需要配置 Satori 服务器
func needsSatoriConfig(conf *Config) bool {
	return conf.Satori.Server.Host == "" || conf.Satori.Server.Port == 0
}

// promptConnectionConfig 提示用户选择连接方式
func promptConnectionConfig(conf *Config) error {
	connectPrompt := &survey.Select{
		Message: "选择开放平台连接方式:",
		Options: []string{"WebSocket", "WebHook"},
		Default: "WebHook",
		Help:    "目前 QQ 开放平台已逐渐取消对 WebSocket 的支持，建议使用 WebHook 连接方式",
	}

	var connectAnswer string
	if err := survey.AskOne(connectPrompt, &connectAnswer); err != nil {
		return err
	}

	// 根据用户选择的连接方式进行配置
	if connectAnswer == "WebSocket" {
		conf.Account.WebSocket.Enable = true
		conf.Account.WebHook.Enable = false
		return promptAccountWebSocketConfig(conf)
	} else {
		conf.Account.WebSocket.Enable = false
		conf.Account.WebHook.Enable = true
		return promptAccountWebHookConfig(conf)
	}
}

// promptAccountBasicConfig 提示用户输入基本账号配置（不包括连接方式）
func promptAccountBasicConfig(conf *Config) error {
	questions := []*survey.Question{
		{
			Name: "bot_id",
			Prompt: &survey.Input{
				Message: "机器人 QQ 号:",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的机器人 QQ 号",
				Default: func() string {
					if conf.Account.BotID != 0 {
						return fmt.Sprintf("%d", conf.Account.BotID)
					}
					return ""
				}(),
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if _, err := strconv.ParseUint(str, 10, 64); err != nil {
						return fmt.Errorf("无效的机器人 QQ 号，请输入一个有效的数字")
					}
				}
				return nil
			},
		},
		{
			Name: "app_id",
			Prompt: &survey.Input{
				Message: "AppID(机器人 ID ):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 AppID",
				Default: func() string {
					if conf.Account.AppID != 0 {
						return fmt.Sprintf("%d", conf.Account.AppID)
					}
					return ""
				}(),
			},
			Validate: func(val interface{}) error {
				if str, ok := val.(string); ok {
					if _, err := strconv.ParseUint(str, 10, 64); err != nil {
						return fmt.Errorf("无效的 AppID，请输入一个有效的数字")
					}
				}
				return nil
			},
		},
		{
			Name: "token",
			Prompt: &survey.Input{
				Message: "Token(机器人令牌):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 Token",
				Default: conf.Account.Token,
			},
			Validate: survey.Required,
		},
		{
			Name: "app_secret",
			Prompt: &survey.Password{
				Message: "AppSecret(机器人密钥):",
				Help:    "通过 QQ 开放平台-管理-开发设置获取到的 AppSecret",
			},
			Validate: survey.Required,
		},
	}

	answer := struct {
		BotID     uint64 `survey:"bot_id"`
		AppID     uint64 `survey:"app_id"`
		Token     string `survey:"token"`
		AppSecret string `survey:"app_secret"`
	}{}

	if err := survey.Ask(questions, &answer); err != nil {
		return err
	}

	conf.Account.BotID = answer.BotID
	conf.Account.AppID = answer.AppID
	conf.Account.Token = answer.Token
	conf.Account.AppSecret = answer.AppSecret

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

const intentsDocs = `
      %s- "GUILDS"                  # 频道事件，该事件是默认订阅的
      %s- "GUILD_MEMBERS"           # 频道成员事件，该事件是默认订阅的
      %s- "GUILD_MESSAGES"          # 消息事件，仅 私域 机器人能够设置此 intents
      %s- "GUILD_MESSAGE_REACTIONS" # 频道消息表态事件
      %s- "DIRECT_MESSAGE"          # 频道私信事件
      %s- "GROUP_AND_C2C_EVENT"     # 单聊/群聊消息事件
      %s- "INTERACTION"             # 互动事件
      %s- "MESSAGE_AUDIT"           # 消息审核事件
      %s- "FORUMS_EVENT"            # 论坛事件，仅 私域 机器人能够设置此 intents
      %s- "AUDIO_ACTION"            # 音频机器人事件
      %s- "PUBLIC_GUILD_MESSAGES"   # 公域消息事件，该事件是默认订阅的`

func dumpIntents(intents []string) string {
	set := make(map[string]bool)
	for _, intent := range intents {
		set[intent] = true
	}

	sharp := func(flag bool) string {
		if flag {
			return ""
		}
		return "#"
	}

	return fmt.Sprintf(
		intentsDocs,
		sharp(set["GUILDS"]),
		sharp(set["GUILD_MEMBERS"]),
		sharp(set["GUILD_MESSAGES"]),
		sharp(set["GUILD_MESSAGE_REACTIONS"]),
		sharp(set["DIRECT_MESSAGE"]),
		sharp(set["GROUP_AND_C2C_EVENT"]),
		sharp(set["INTERACTION"]),
		sharp(set["MESSAGE_AUDIT"]),
		sharp(set["FORUMS_EVENT"]),
		sharp(set["AUDIO_ACTION"]),
		sharp(set["PUBLIC_GUILD_MESSAGES"]),
	)
}
