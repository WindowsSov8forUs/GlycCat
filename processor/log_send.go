package processor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func messageLogDestination(request *server.Request[server.MessageCreateParam]) (string, string) {
	id := request.Params.ChannelID
	direct, _ := request.Params.Referrer.ValueOr(nil)["direct"].(bool)
	if request.Platform == "qqguild" {
		if direct || strings.Contains(id, "_") {
			return "私聊频道", id
		}
		return "频道", id
	}
	if direct || strings.HasPrefix(id, "private:") {
		return "用户", strings.TrimPrefix(id, "private:")
	}
	return "群", id
}

// logSendAttempt 只表示发送尝试；正文使用预处理前的内容，避免输出转码后的 base64。
func logSendAttempt(request *server.Request[server.MessageCreateParam], content string) {
	kind, id := messageLogDestination(request)
	log.Infof("发送消息到%s %s : %s", kind, id, shortLogContent(log.SafeText(renderLogContent(content, ""))))
}

// logSendResult 观察 SDK 原结果，不修改响应、不补造已发送片段，也不进行重试。
func logSendResult(request *server.Request[server.MessageCreateParam], result any, err error) {
	kind, id := messageLogDestination(request)
	if err != nil {
		log.Errorf("发送消息到%s %s 时出错: %v", kind, id, err)
		return
	}
	var response *server.Response
	switch value := result.(type) {
	case *server.Response:
		response = value
	case server.Response:
		response = &value
	case []*message.Message:
		log.Debugf("消息发送完成，共 %d 条。", len(value))
		return
	}
	if response == nil || responseSucceeded(response) {
		return
	}
	var body struct {
		Error    string             `json:"error"`
		Messages []*message.Message `json:"messages"`
	}
	// 只读取声明的错误与消息列表，不把整个响应正文打印到日志。
	cause := fmt.Sprintf("开放平台返回状态码 %d", response.StatusCode)
	count := 0
	if json.Unmarshal(response.Body, &body) == nil {
		if body.Error != "" {
			cause = body.Error
		}
		for _, item := range body.Messages {
			if item != nil && item.Id != "" {
				count++
			}
		}
	}
	if count > 0 {
		log.Warnf("消息仅部分发送成功，已发送 %d 条，后续发送失败: %s", count, cause)
		return
	}
	log.Errorf("发送消息到%s %s 时出错: %s", kind, id, cause)
}
