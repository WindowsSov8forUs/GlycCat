package processor

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	xhtml "golang.org/x/net/html"
)

const maxContentBytes = 32 * 1024 * 1024

type contentTag struct {
	start, end    int
	parent        int
	token         xhtml.Token
	skipResources bool
}

// scanContentTags 只定位标签，不重建消息树，未修改的文本和扩展属性保持原样。
func scanContentTags(ctx context.Context, content string) ([]contentTag, error) {
	if len(content) > maxContentBytes {
		return nil, fmt.Errorf("消息内容超过 32 MiB 上限")
	}
	tokenizer := xhtml.NewTokenizer(strings.NewReader(content))
	tokenizer.SetMaxBuf(maxContentBytes + 1)
	var tags []contentTag
	var stack []int
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind := tokenizer.Next()
		start := offset
		offset += len(tokenizer.Raw())
		if kind == xhtml.ErrorToken {
			if tokenizer.Err() == io.EOF {
				return tags, nil
			}
			return nil, fmt.Errorf("读取消息标签失败: %w", tokenizer.Err())
		}
		// Satori 不是浏览器 HTML，不对 title 等扩展标签套用原始文本模式。
		tokenizer.NextIsNotRawText()
		if kind != xhtml.StartTagToken && kind != xhtml.SelfClosingTagToken && kind != xhtml.EndTagToken {
			continue
		}
		token := tokenizer.Token()
		if kind == xhtml.EndTagToken {
			if len(stack) > 0 && tags[stack[len(stack)-1]].token.Data == token.Data {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(tags) >= 65536 || len(stack) >= 256 {
			return nil, fmt.Errorf("消息标签数量或嵌套深度超过上限")
		}
		tag := contentTag{start: start, end: offset, parent: -1, token: token}
		if len(stack) > 0 {
			tag.parent = stack[len(stack)-1]
			parent := tags[tag.parent]
			tag.skipResources = parent.skipResources || hidesResources(parent.token.Data)
		}
		tags = append(tags, tag)
		if kind == xhtml.StartTagToken {
			stack = append(stack, len(tags)-1)
		}
	}
}

func hidesResources(tag string) bool {
	switch tag {
	case "quote", "text", "at", "sharp", "br", "img", "image", "audio", "video", "file",
		"qq:passive", "qq:ark", "markdown", "qq:button-group", "button":
		return true
	default:
		return false
	}
}

// normalizeLegacyQuotes 兼容旧 <quote><message id="..."/></quote> 写法。
// 只取直接子元素中首个非空消息 ID；显式 quote.id 优先，引用不生成被动回复凭据。
func normalizeLegacyQuotes(ctx context.Context, content string) (string, error) {
	if !strings.Contains(content, "<quote") {
		return content, nil
	}
	tags, err := scanContentTags(ctx, content)
	if err != nil {
		return "", err
	}
	changes := make(map[int]string)
	for _, tag := range tags {
		if tag.token.Data != "message" || tag.parent < 0 {
			continue
		}
		parent := tags[tag.parent]
		if parent.token.Data != "quote" {
			continue
		}
		if _, present := tagAttribute(parent.token, "id"); present {
			continue
		}
		if _, changed := changes[tag.parent]; changed {
			continue
		}
		id, present := tagAttribute(tag.token, "id")
		if !present || id == "" {
			continue
		}
		updated, err := replaceTagAttribute(content[parent.start:parent.end], "id", id)
		if err != nil {
			return "", err
		}
		changes[tag.parent] = updated
	}
	return applyContentChanges(content, tags, changes)
}

func tagAttribute(token xhtml.Token, name string) (string, bool) {
	for i := len(token.Attr) - 1; i >= 0; i-- {
		if token.Attr[i].Key == name {
			return token.Attr[i].Val, true
		}
	}
	return "", false
}

// replaceTagAttribute 只替换指定属性，保留布尔属性、未知扩展和原有标签写法。
func replaceTagAttribute(raw, name, value string) (string, error) {
	start, end := -1, -1
	i := 1
	for i < len(raw) && !tagSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && tagSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] == '/' || raw[i] == '>' {
			break
		}
		keyStart := i
		for i < len(raw) && !tagSpace(raw[i]) && raw[i] != '=' && raw[i] != '/' && raw[i] != '>' {
			i++
		}
		if i == keyStart {
			return "", fmt.Errorf("消息标签属性格式无效")
		}
		key := raw[keyStart:i]
		keyEnd := i
		for i < len(raw) && tagSpace(raw[i]) {
			i++
		}
		attributeEnd := keyEnd
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && tagSpace(raw[i]) {
				i++
			}
			if i >= len(raw) {
				return "", fmt.Errorf("消息标签属性值缺失")
			}
			if raw[i] == '\'' || raw[i] == '"' {
				quote := raw[i]
				i++
				for i < len(raw) && raw[i] != quote {
					i++
				}
				if i >= len(raw) {
					return "", fmt.Errorf("消息标签属性未闭合")
				}
				i++
			} else {
				for i < len(raw) && !tagSpace(raw[i]) && raw[i] != '>' {
					i++
				}
			}
			attributeEnd = i
		}
		if key == name {
			if start >= 0 {
				return "", fmt.Errorf("消息标签的 %s 属性重复", name)
			}
			start, end = keyStart, attributeEnd
		}
	}
	attribute := name + "=\"" + html.EscapeString(value) + "\""
	if start >= 0 {
		return raw[:start] + attribute + raw[end:], nil
	}
	i = len(raw) - 1
	if i < 0 || raw[i] != '>' {
		return "", fmt.Errorf("消息标签未闭合")
	}
	if i > 0 && raw[i-1] == '/' {
		i--
	}
	return raw[:i] + " " + attribute + raw[i:], nil
}

func tagSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\f'
}

func applyContentChanges(content string, tags []contentTag, changes map[int]string) (string, error) {
	if len(changes) == 0 {
		return content, nil
	}
	size := len(content)
	for index, replacement := range changes {
		size += len(replacement) - (tags[index].end - tags[index].start)
	}
	if size > maxContentBytes {
		return "", fmt.Errorf("处理后的消息内容超过 32 MiB 上限")
	}
	var result strings.Builder
	result.Grow(size)
	offset := 0
	for index, tag := range tags {
		if replacement, changed := changes[index]; changed {
			result.WriteString(content[offset:tag.start])
			result.WriteString(replacement)
			offset = tag.end
		}
	}
	result.WriteString(content[offset:])
	return result.String(), nil
}
