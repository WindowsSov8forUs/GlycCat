package processor

import (
	"fmt"
	"time"

	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/WindowsSov8forUs/glyccat/operation"

	"github.com/satori-protocol-go/satori-model-go/pkg/channel"
	"github.com/satori-protocol-go/satori-model-go/pkg/guild"
	"github.com/satori-protocol-go/satori-model-go/pkg/guildmember"
	"github.com/satori-protocol-go/satori-model-go/pkg/guildrole"
	"github.com/satori-protocol-go/satori-model-go/pkg/message"
	"github.com/satori-protocol-go/satori-model-go/pkg/user"
	"github.com/tencent-connect/botgo/dto"
)

// ProcessGuildATMessage 处理群组 AT 消息
func (p *Processor) ProcessGuildATMessage(payload *dto.Payload, data *dto.ATMessageData) error {
	// 打印消息日志
	printGuildATMessage(data)

	// 构建事件数据
	var event *operation.Event

	// 获取事件 ID
	id := SaveEventID(payload.ID)

	// 将事件字符串转换为时间戳
	t, err := time.Parse(time.RFC3339, string(data.Timestamp))
	if err != nil {
		return fmt.Errorf("解析时间戳时出错: %v", err)
	}

	// 构建 channel
	channel := &channel.Channel{
		Id:   data.ChannelID,
		Type: channel.ChannelTypeText,
	}

	// 构建 guild
	guild := &guild.Guild{
		Id: data.GuildID,
	}

	// 构建 member
	joinedTime, err := data.Member.JoinedAt.Time()
	if err != nil {
		return fmt.Errorf("解析加入时间时出错: %v", err)
	}
	member := &guildmember.GuildMember{
		Nick:     data.Member.Nick,
		JoinedAt: joinedTime.UnixMilli(),
	}

	// 构建 message
	message := &message.Message{
		Id:       data.ID,
		CreateAt: t.UnixMilli(),
	}
	// 转换消息格式
	content := ConvertToMessageContent(data)
	message.Content = content

	// 构建 user
	user := &user.User{
		Id:     data.Author.ID,
		Name:   data.Author.Username,
		Avatar: data.Author.Avatar,
		IsBot:  data.Author.Bot,
	}

	// 填充事件数据
	event = &operation.Event{
		Sn:        id,
		Type:      operation.EventTypeMessageCreated,
		Timestamp: t.UnixMilli(),
		Login:     buildNonLoginEventLogin("qqguild"),
		Channel:   channel,
		Guild:     guild,
		Member:    member,
		Message:   message,
		User:      user,
	}

	// 需要处理可能的 Member.Roles 为空的情况
	//
	// 傻逼腾讯
	//
	// 构建 role
	if len(data.Member.Roles) > 0 {
		role := &guildrole.GuildRole{
			Id: data.Member.Roles[0],
		}
		event.Role = role
	}

	// 上报消息到 Satori 应用
	return p.BroadcastEvent(event)
}

func printGuildATMessage(data *dto.ATMessageData) {
	// 构建用户名称
	var userName string
	if data.Member.Nick != "" {
		userName = fmt.Sprintf("%s(%s)", data.Member.Nick, data.Author.ID)
	} else if data.Author.Username != "" {
		userName = fmt.Sprintf("%s(%s)", data.Author.Username, data.Author.ID)
	} else {
		userName = data.Author.ID
	}

	// 构建消息日志
	msgContent := getMessageLog(data)

	log.Infof("收到来自频道 %s 的子频道 %s 的用户 %s 的消息: %s", data.GuildID, data.ChannelID, userName, msgContent)
}
