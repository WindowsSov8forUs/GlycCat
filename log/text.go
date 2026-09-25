package log

import (
	stdlog "log"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
)

var secretText atomic.Pointer[strings.Replacer]
var resourceText = regexp.MustCompile(`(?:https?://|internal:|data:)[^\s<>"']+`)
var credentialText = regexp.MustCompile(`(?i)(\b(?:authorization|access[_-]?token|app[_-]?secret|client[_-]?secret|secret|token)\b["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:Bearer|Bot)\s+[^\s,;}\]]+|[^\s,;}\]]+)`)

// SetSecrets 在开始运行前登记应用凭据，不把令牌恢复到历史的明文日志中。
func SetSecrets(values ...string) {
	unique := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			unique[value] = true
			unique[url.QueryEscape(value)] = true
		}
	}
	var secrets []string
	for value := range unique {
		secrets = append(secrets, value)
	}
	// 长值优先，避免短令牌是长令牌前缀时留下后半段。
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	var pairs []string
	for _, value := range secrets {
		pairs = append(pairs, value, "[已隐藏]")
	}
	secretText.Store(strings.NewReplacer(pairs...))
}

// SafeText 保留可读正文，只去除凭据、资源访问参数并转义控制字符。
// 时间戳及等级颜色由外层 Formatter 生成，不受这里的正文处理影响。
func SafeText(text string) string {
	if replacer := secretText.Load(); replacer != nil {
		text = replacer.Replace(text)
	}
	text = credentialText.ReplaceAllString(text, "${1}[已隐藏]")
	text = resourceText.ReplaceAllStringFunc(text, func(raw string) string {
		if strings.HasPrefix(raw, "data:") {
			return "[数据资源]"
		}
		if strings.HasPrefix(raw, "internal:") {
			return "[内部资源]"
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "[资源地址]"
		}
		if strings.Contains(u.Path, "/_tmp/") || strings.Contains(u.Path, "/proxy/") {
			return "[内部资源地址]"
		}
		u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
		u.ForceQuery = false
		return u.String()
	})
	var result strings.Builder
	for _, char := range text {
		switch char {
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		default:
			if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' || (char >= '\u202a' && char <= '\u202e') || (char >= '\u2066' && char <= '\u2069') {
				quoted := strconv.QuoteRuneToASCII(char)
				result.WriteString(quoted[1 : len(quoted)-1])
			} else {
				result.WriteRune(char)
			}
		}
	}
	return result.String()
}

// HTTPErrorLogger 接回 net/http 的隐式错误输出，不改动 HTTP 处理行为。
func HTTPErrorLogger() *stdlog.Logger {
	return stdlog.New(httpErrorWriter{}, "", 0)
}

type httpErrorWriter struct{}

func (httpErrorWriter) Write(data []byte) (int, error) {
	Errorf("HTTP 服务器处理连接时出错: %s", strings.TrimSpace(string(data)))
	return len(data), nil
}
