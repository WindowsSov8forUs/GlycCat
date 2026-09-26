package config

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// intentOptions 统一配置校验、交互选择和注释清单；别名不代表额外订阅。
var intentOptions = []struct {
	Name        string
	Description string
	Aliases     []string
}{
	{"GUILDS", "频道及子频道创建、更新、删除", nil},
	{"GUILD_MEMBERS", "频道成员加入、更新、退出", nil},
	{"GUILD_MESSAGES", "频道全部消息及撤回，仅私域机器人可用", nil},
	{"GUILD_MESSAGE_REACTIONS", "频道消息表态添加、移除", []string{"GUILD_MESSAGE_REACTION"}},
	{"DIRECT_MESSAGE", "频道私信及撤回，不是普通 QQ 单聊", []string{"DIRECT_MESSAGES"}},
	{"OPEN_FORUM_EVENT", "开放论坛主题、帖子和回复事件", []string{"OPEN_FORUMS_EVENT"}},
	{"AUDIO_OR_LIVE_CHANNEL_MEMBER", "音视频、直播子频道成员进出", []string{"AUDIO_LIVE_MEMBER"}},
	{"GROUP_MEMBER_EVENT", "QQ 群成员加入、退出；入群申请另需群管理员", []string{"GROUP_MEMBERS"}},
	{"GROUP_AND_C2C_EVENT", "QQ 群聊、单聊及相关事件；全量群消息需开启接收所有消息", []string{"C2C_GROUP_AT_MESSAGES", "USER_MESSAGES"}},
	{"INTERACTION", "按钮、快捷菜单、消息反馈及授权等互动事件", nil},
	{"MESSAGE_AUDIT", "消息审核通过、拒绝", nil},
	{"FORUMS_EVENT", "私域论坛主题、帖子、回复及审核事件", []string{"FORUM_EVENT"}},
	{"AUDIO_ACTION", "音频播放开始、结束及上下麦事件", nil},
	{"PUBLIC_GUILD_MESSAGES", "频道 @ 机器人消息及相关撤回", []string{"AT_MESSAGES"}},
}

func canonicalIntentName(value string) (string, bool) {
	name := strings.ToUpper(strings.TrimSpace(value))
	for _, option := range intentOptions {
		if name == option.Name {
			return option.Name, true
		}
		for _, alias := range option.Aliases {
			if name == alias {
				return option.Name, true
			}
		}
	}
	return "", false
}

func intentNames() []string {
	names := make([]string, 0, len(intentOptions))
	for _, option := range intentOptions {
		names = append(names, option.Name)
	}
	return names
}

// formatIntentList 只替换编码后 YAML 中定位到的订阅段，不匹配用户值中的同名文本。
// 已选项按同一订阅位归一，未选项保留为带说明的注释；不自动添加订阅。
func formatIntentList(data []byte, names []string) ([]byte, error) {
	selected := make(map[string]bool, len(names))
	for _, value := range names {
		name, ok := canonicalIntentName(value)
		if !ok {
			return nil, fmt.Errorf("存在不支持的 WebSocket 事件订阅 %q，请核对 intents 名称", value)
		}
		selected[name] = true
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	node := document.Content[0]
	var key *yaml.Node
	for _, name := range []string{"account", "websocket", "intents"} {
		found := false
		for i := 0; node.Kind == yaml.MappingNode && i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == name {
				key, node = node.Content[i], node.Content[i+1]
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("编码后的配置缺少 account.websocket.intents")
		}
	}
	lines := strings.SplitAfter(string(data), "\n")
	start, end := key.Line-1, key.Line
	indent := strings.Repeat(" ", key.Column-1)
	for end < len(lines) {
		text := strings.TrimSpace(lines[end])
		spaces := len(lines[end]) - len(strings.TrimLeft(lines[end], " "))
		if text != "" && !strings.HasPrefix(text, "#") && spaces <= len(indent) {
			break
		}
		end++
	}
	var output bytes.Buffer
	output.WriteString(strings.Join(lines[:start], ""))
	output.WriteString(indent + "intents:\n")
	for _, option := range intentOptions {
		entry := fmt.Sprintf("- %q", option.Name)
		if !selected[option.Name] {
			entry = "# " + entry
		}
		fmt.Fprintf(&output, "%s  %-38s # %s\n", indent, entry, option.Description)
	}
	output.WriteByte('\n')
	output.WriteString(strings.Join(lines[end:], ""))
	return output.Bytes(), nil
}
