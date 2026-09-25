package processor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	xhtml "golang.org/x/net/html"
)

// nativeLogData 只读取日志需要的字段，兼容数据体和 SDK 保留的完整 QQ 信封。
// 不修改事件，不查询网络，也不把原生信封或回复凭据作为日志正文。
func nativeLogData(evt *event.Event) map[string]any {
	var data map[string]any
	switch value := evt.Data_.(type) {
	case map[string]any:
		data = value
	default:
		var raw []byte
		switch value := value.(type) {
		case json.RawMessage:
			raw = value
		case []byte:
			raw = value
		default:
			var err error
			raw, err = json.Marshal(value)
			if err != nil {
				return nil
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&data) != nil {
			return nil
		}
	}
	if _, envelope := data["op"]; envelope {
		if body, ok := data["d"].(map[string]any); ok {
			return body
		}
	}
	return data
}

func logValue(data map[string]any, keys ...string) string {
	var value any = data
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	switch value := value.(type) {
	case string:
		return value
	case json.Number:
		return string(value)
	case float64:
		return fmt.Sprintf("%.0f", value)
	case int:
		return fmt.Sprint(value)
	}
	return ""
}

func firstLogValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func logDisplayName(id, name, fallback string) string {
	if name != "" && id != "" && name != id {
		return name + "(" + id + ")"
	}
	return firstLogValue(name, id, fallback)
}

// logResource 沿用历史 [图片](地址) 等样式；内部资源和数据载荷只显示类型。
// 不输出 URL 用户信息、查询参数、片段或代理入口中的能力型资源地址。
func logResource(kind, source string) string {
	label := "[" + kind + "]"
	if source == "" || strings.HasPrefix(source, "data:") || strings.HasPrefix(source, "internal:") {
		return label
	}
	if !strings.Contains(source, "://") {
		source = "https://" + strings.TrimPrefix(source, "//")
	}
	u, err := url.Parse(source)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return label
	}
	if strings.Contains(u.Path, "/_tmp/") || strings.Contains(u.Path, "/proxy/") {
		return label
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
	u.ForceQuery = false
	return label + "(" + u.String() + ")"
}

// renderLogContent 只渲染展示内容，绝不复用发送器或读取 src 资源。
// quote 和平台扩展载荷不展开，防止引用正文、base64 和被动回复凭据重复输出。
func renderLogContent(content, skipMention string) string {
	if len(content) > maxContentBytes {
		return "[消息超过展示处理上限]"
	}
	tokenizer := xhtml.NewTokenizer(strings.NewReader(content))
	tokenizer.SetMaxBuf(maxContentBytes + 1)
	var output strings.Builder
	var stack []string
	hiddenDepth := 0
	for {
		kind := tokenizer.Next()
		if kind == xhtml.ErrorToken {
			if tokenizer.Err() != io.EOF {
				output.WriteString("[消息格式无法完整显示]")
			}
			return strings.TrimSpace(output.String())
		}
		tokenizer.NextIsNotRawText()
		if kind == xhtml.TextToken {
			if hiddenDepth == 0 {
				output.Write(tokenizer.Text())
			}
			continue
		}
		if kind == xhtml.CommentToken || kind == xhtml.DoctypeToken {
			continue
		}
		token := tokenizer.Token()
		if kind == xhtml.EndTagToken {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == token.Data {
					stack = stack[:i]
					if hiddenDepth > i {
						hiddenDepth = 0
					}
					break
				}
			}
			continue
		}
		if kind != xhtml.StartTagToken && kind != xhtml.SelfClosingTagToken {
			continue
		}
		if len(stack) >= 256 {
			output.WriteString("[消息嵌套超过展示处理上限]")
			return output.String()
		}
		attr := func(key string) string { value, _ := tagAttribute(token, key); return value }
		hide := false
		if hiddenDepth == 0 {
			switch token.Data {
			case "img", "image":
				output.WriteString(logResource("图片", attr("src")))
				hide = true
			case "audio", "video", "file":
				name := map[string]string{"audio": "语音", "video": "视频", "file": "文件"}[token.Data]
				output.WriteString(logResource(name, attr("src")))
				hide = true
			case "at":
				if attr("type") == "all" {
					output.WriteString("@全体成员")
				} else if skipMention == "" || attr("id") != skipMention {
					output.WriteString("@" + firstLogValue(attr("name"), attr("id"), "未知用户"))
				}
				hide = true
			case "sharp":
				output.WriteString("#" + firstLogValue(attr("id"), "未知子频道"))
				hide = true
			case "quote":
				if id := attr("id"); id != "" {
					output.WriteString("[回复消息" + id + "]")
				} else {
					output.WriteString("[回复消息]")
				}
				hide = true
			case "qq:passive", "qq:button-group":
				hide = true
			case "qq:ark", "qq:ark-data":
				output.WriteString("[ark]")
				hide = true
			case "qq:embed":
				output.WriteString("[embed](" + attr("title") + ")")
				hide = true
			case "emoji", "qq:emoji", "chronocat:emoji":
				output.WriteString("[emoji" + attr("id") + "]")
				hide = true
			case "button":
				output.WriteString("[按钮]")
				hide = true
			case "text":
				output.WriteString(firstLogValue(attr("text"), attr("content")))
			case "br":
				output.WriteString("\n")
			}
		}
		if kind == xhtml.StartTagToken {
			stack = append(stack, token.Data)
			if hide && hiddenDepth == 0 {
				hiddenDepth = len(stack)
			}
		}
	}
}

var nativeLogMention = regexp.MustCompile(`@everyone|<@!?([^<>\s]+)>|<#([^<>\s]+)>|<emoji:([^<>\s]+)>`)

// receivedLogContent 承接 56972aee 的 getMessageLog，不修改协议消息内容。
func receivedLogContent(evt *event.Event, data map[string]any) string {
	var selfID, selfName string
	if evt.Login != nil && evt.Login.User != nil {
		selfID = evt.Login.User.Id
		selfName = firstLogValue(evt.Login.User.Name, selfID)
	}
	groupAt := evt.Type_ == "GROUP_AT_MESSAGE_CREATE"
	var parts []string
	_, nativeContent := data["content"]
	nativeContent = nativeContent || data["attachments"] != nil || data["embeds"] != nil || data["ark"] != nil || data["markdown"] != nil
	if nativeContent {
		mentions := make(map[string]string)
		if list, ok := data["mentions"].([]any); ok {
			for _, item := range list {
				if object, ok := item.(map[string]any); ok {
					id := logValue(object, "id")
					mentions[id] = firstLogValue(logValue(object, "username"), id)
				}
			}
		}
		text := nativeLogMention.ReplaceAllStringFunc(firstLogValue(logValue(data, "content"), logValue(data, "markdown", "content")), func(value string) string {
			match := nativeLogMention.FindStringSubmatch(value)
			switch {
			case value == "@everyone":
				if enabled, _ := data["mention_everyone"].(bool); enabled {
					return "@全体成员"
				}
				return value
			case match[1] != "":
				if groupAt && selfID != "" && match[1] == selfID {
					return ""
				}
				return "@" + firstLogValue(mentions[match[1]], match[1])
			case match[2] != "":
				return "#" + match[2]
			default:
				return "[emoji" + match[3] + "]"
			}
		})
		parts = append(parts, text)
		if list, ok := data["attachments"].([]any); ok {
			for _, item := range list {
				object, ok := item.(map[string]any)
				if !ok {
					continue
				}
				kind := "文件"
				switch contentType := logValue(object, "content_type"); {
				case strings.HasPrefix(contentType, "image"):
					kind = "图片"
				case strings.HasPrefix(contentType, "audio"):
					kind = "语音"
				case strings.HasPrefix(contentType, "video"):
					kind = "视频"
				}
				parts = append(parts, logResource(kind, logValue(object, "url")))
			}
		}
		if list, ok := data["embeds"].([]any); ok {
			for _, item := range list {
				if object, ok := item.(map[string]any); ok {
					parts = append(parts, "[embed]("+logValue(object, "title")+")")
				}
			}
		}
		if id := logValue(data, "ark", "template_id"); id != "" {
			parts = append(parts, "[ark]("+id+")")
		}
		if id := logValue(data, "message_reference", "message_id"); id != "" {
			parts = append(parts, "[回复消息"+id+"]")
		}
	} else if evt.Message != nil {
		skip := ""
		if groupAt {
			skip = selfID
		}
		parts = append(parts, renderLogContent(evt.Message.Content, skip))
	}
	content := strings.TrimSpace(strings.Join(parts, " "))
	if groupAt && selfName != "" && content != "@"+selfName && !strings.HasPrefix(content, "@"+selfName+" ") {
		// 56972aee 将群 @ 事件的机器人名称放在正文开头；普通群消息不添加。
		content = strings.TrimSpace("@" + selfName + " " + content)
	}
	if content == "" {
		return "[空消息]"
	}
	return content
}

// shortLogContent 保留历史前 40 / 后 10 的省略外观，按字符截取以避免截坏中文。
func shortLogContent(content string) string {
	runes := []rune(content)
	if len(runes) > 50 {
		return string(runes[:40]) + "..." + string(runes[len(runes)-10:])
	}
	return content
}
