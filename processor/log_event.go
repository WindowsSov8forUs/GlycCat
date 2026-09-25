package processor

import (
	"fmt"
	"strings"

	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

// logEvent 与缓存无关，只在唯一 Publisher 中调用，覆盖 qq 和 qqguild。
// 正文承接 56972aee 以前的 print*Event；不恢复旧事件处理、网络查询和 ACK。
func (a *Adapter) logEvent(evt *event.Event) {
	if evt == nil {
		return
	}
	if evt.Type == event.EventTypeLoginAdded || evt.Type == event.EventTypeLoginUpdated || evt.Type == event.EventTypeLoginRemoved {
		if evt.Login == nil || evt.Login.User == nil {
			return
		}
		if a.logStates == nil {
			a.logStates = make(map[string]login.LoginStatus)
		}
		key := evt.Login.User.Id
		if evt.Type == event.EventTypeLoginRemoved {
			delete(a.logStates, key)
			return
		}
		previous, known := a.logStates[key]
		a.logStates[key] = evt.Login.Status
		if evt.Login.Status == login.LoginStatusOnline && (!known || previous != login.LoginStatusOnline) {
			log.Infof("欢迎使用机器人：%s ！", logDisplayName(key, evt.Login.User.Name, "未知机器人"))
		}
		return
	}
	if text := eventLogText(evt); text != "" {
		log.Info(text)
	}
}

func eventLogText(evt *event.Event) string {
	data := nativeLogData(evt)
	kind := firstLogValue(evt.Type_, string(evt.Type))
	platform := ""
	if evt.Login != nil {
		platform = evt.Login.Platform
	}
	var userID, userName, operatorID, operatorName, guildID, guildName, channelID string
	messageChannel := evt.Channel
	// 兼容资源在 Message 内或提升到 Event 顶层的两种结构，仅读取不改写。
	if evt.Message != nil {
		if evt.Message.User != nil {
			userID, userName = evt.Message.User.Id, evt.Message.User.Name
		}
		if evt.Message.Member != nil {
			userName = firstLogValue(evt.Message.Member.Nick, userName)
		}
		if evt.Message.Guild != nil {
			guildID, guildName = evt.Message.Guild.Id, evt.Message.Guild.Name
		}
		if messageChannel == nil {
			messageChannel = evt.Message.Channel
		}
	}
	if evt.User != nil {
		userID, userName = evt.User.Id, evt.User.Name
	}
	if evt.Member != nil {
		userName = firstLogValue(evt.Member.Nick, userName)
	}
	if evt.Operator != nil {
		operatorID, operatorName = evt.Operator.Id, evt.Operator.Name
	}
	if evt.Guild != nil {
		guildID, guildName = evt.Guild.Id, evt.Guild.Name
	}
	if messageChannel != nil {
		channelID = messageChannel.Id
	}
	userID = firstLogValue(logValue(data, "author", "member_openid"), logValue(data, "author", "user_openid"), logValue(data, "user", "id"), logValue(data, "author", "id"), logValue(data, "user_openid"), logValue(data, "member_openid"), logValue(data, "openid"), logValue(data, "user_id"), userID)
	userName = firstLogValue(logValue(data, "member", "nick"), logValue(data, "nick"), logValue(data, "user", "username"), logValue(data, "author", "username"), userName)
	operatorID = firstLogValue(logValue(data, "op_member_openid"), logValue(data, "op_user_id"), logValue(data, "op_user", "id"), operatorID)
	operatorName = firstLogValue(logValue(data, "op_user", "username"), operatorName)
	guildID = firstLogValue(logValue(data, "group_openid"), logValue(data, "group_id"), logValue(data, "guild_id"), logValue(data, "message", "guild_id"), guildID)
	channelID = firstLogValue(logValue(data, "channel_id"), logValue(data, "message", "channel_id"), channelID)
	person := logDisplayName(userID, userName, "未知用户")
	operator := logDisplayName(operatorID, operatorName, "未知用户")
	group := firstLogValue(guildID, "未知群组")
	channelName := firstLogValue(channelID, "未知子频道")

	if evt.Type == event.EventTypeMessageCreated {
		content := receivedLogContent(evt, data)
		switch {
		case kind == "DIRECT_MESSAGE_CREATE" || (platform == "qqguild" && messageChannel != nil && messageChannel.Type == channel.ChannelTypeDirect):
			return fmt.Sprintf("收到来自用户 %s 的私聊频道消息: %s", person, content)
		case platform == "qqguild":
			return fmt.Sprintf("收到来自频道 %s 的子频道 %s 的用户 %s 的消息: %s", group, channelName, person, content)
		case kind == "C2C_MESSAGE_CREATE" || strings.HasPrefix(channelID, "private:"):
			return fmt.Sprintf("收到来自用户 %s 的私聊消息: %s", firstLogValue(userID, strings.TrimPrefix(channelID, "private:"), "未知用户"), content)
		default:
			return fmt.Sprintf("收到来自群 %s 用户 %s 的消息: %s", firstLogValue(guildID, channelID, "未知群组"), firstLogValue(userID, "未知用户"), content)
		}
	}

	switch kind {
	case "GROUP_ADD_ROBOT":
		if operatorID != "" {
			return fmt.Sprintf("机器人被 %s 添加进了群组 %s", operator, group)
		}
		return fmt.Sprintf("机器人已加入群组 %s", group)
	case "GROUP_DEL_ROBOT":
		if operatorID != "" {
			return fmt.Sprintf("机器人被 %s 移出了群组 %s", operator, group)
		}
		return fmt.Sprintf("机器人已退出群组 %s", group)
	case "GUILD_CREATE", "GUILD_UPDATE", "GUILD_DELETE":
		name := logDisplayName(firstLogValue(logValue(data, "id"), guildID), firstLogValue(logValue(data, "name"), guildName), "未知频道")
		switch kind {
		case "GUILD_CREATE":
			// 接入通知不等于用户刚创建频道，不能照抄旧实现的因果判断。
			return fmt.Sprintf("已收到频道 %s 的接入通知。", name)
		case "GUILD_DELETE":
			return fmt.Sprintf("已收到频道 %s 的退出通知。", name)
		default:
			if operatorID != "" {
				return fmt.Sprintf("用户 %s 更新了频道 %s 的信息。", operator, name)
			}
			return fmt.Sprintf("频道 %s 的信息已更新。", name)
		}
	case "CHANNEL_CREATE", "CHANNEL_UPDATE", "CHANNEL_DELETE":
		name := logDisplayName(firstLogValue(logValue(data, "id"), channelID), logValue(data, "name"), "未知子频道")
		name = channelLogType(logValue(data, "type")) + " " + name
		action := map[string]string{"CHANNEL_CREATE": "创建", "CHANNEL_UPDATE": "更新", "CHANNEL_DELETE": "删除"}[kind]
		if operatorID == "" {
			return fmt.Sprintf("频道 %s 的 %s 已%s。", group, name, action)
		}
		if kind == "CHANNEL_UPDATE" {
			return fmt.Sprintf("用户 %s 在频道 %s 更新了 %s 的信息。", operator, group, name)
		}
		return fmt.Sprintf("用户 %s 在频道 %s %s了 %s 。", operator, group, action, name)
	case "GUILD_MEMBER_ADD", "GUILD_MEMBER_UPDATE", "GUILD_MEMBER_REMOVE", "GUILD_MEMBER_DELETE", "GROUP_MEMBER_ADD", "GROUP_MEMBER_REMOVE", "GROUP_JOIN_REQUEST":
		place := "频道"
		if strings.HasPrefix(kind, "GROUP_") {
			place = "群组"
		}
		different := operatorID != "" && userID != "" && operatorID != userID
		switch {
		case kind == "GROUP_JOIN_REQUEST":
			return fmt.Sprintf("用户 %s 申请加入群组 %s 。", person, group)
		case strings.HasSuffix(kind, "_ADD"):
			if different {
				return fmt.Sprintf("用户 %s 邀请了用户 %s 加入%s %s 。", operator, person, place, group)
			}
			return fmt.Sprintf("用户 %s 加入了%s %s 。", person, place, group)
		case strings.HasSuffix(kind, "_UPDATE"):
			if different {
				return fmt.Sprintf("频道 %s 的用户 %s 更新了用户 %s 的信息。", group, operator, person)
			}
			if operatorID != "" && operatorID == userID {
				return fmt.Sprintf("频道 %s 的用户 %s 更新了自己的信息。", group, person)
			}
			return fmt.Sprintf("频道 %s 的用户 %s 信息已更新。", group, person)
		default:
			if different {
				return fmt.Sprintf("用户 %s 将用户 %s 移出了%s %s 。", operator, person, place, group)
			}
			return fmt.Sprintf("用户 %s 离开了%s %s 。", person, place, group)
		}
	case "C2C_FRIEND_ADD", "friend-added":
		return fmt.Sprintf("用户 %s 添加了机器人为好友。", person)
	case "C2C_FRIEND_DEL", "friend-removed":
		return fmt.Sprintf("用户 %s 删除了机器人好友。", person)
	case "MESSAGE_DELETE", "PUBLIC_MESSAGE_DELETE", "DIRECT_MESSAGE_DELETE":
		authorID := firstLogValue(logValue(data, "message", "author", "id"), userID)
		authorName := firstLogValue(logValue(data, "message", "member", "nick"), logValue(data, "message", "author", "username"), userName)
		if kind == "DIRECT_MESSAGE_DELETE" {
			if operatorID == "" {
				return "有一条私聊频道消息被撤回。"
			}
			return fmt.Sprintf("用户 %s 撤回了一条私聊频道消息。", operator)
		}
		if operatorID == "" {
			return fmt.Sprintf("频道 %s 的子频道 %s 有一条消息被撤回。", group, channelName)
		}
		if authorID != "" && operatorID != authorID {
			return fmt.Sprintf("频道 %s 的子频道 %s 的用户 %s 撤回了用户 %s 的一条消息。", group, channelName, operator, logDisplayName(authorID, authorName, "未知用户"))
		}
		return fmt.Sprintf("频道 %s 的子频道 %s 的用户 %s 撤回了一条消息。", group, channelName, operator)
	case "MESSAGE_REACTION_ADD", "MESSAGE_REACTION_REMOVE":
		targetType := map[string]string{"0": "消息", "1": "帖子", "2": "评论", "3": "回复"}[logValue(data, "target", "type")]
		target := logDisplayName(logValue(data, "target", "id"), firstLogValue(targetType, "未知目标"), "未知目标")
		emojiType := map[string]string{"1": "系统表情", "2": "emoji表情"}[logValue(data, "emoji", "type")]
		emoji := logDisplayName(logValue(data, "emoji", "id"), firstLogValue(emojiType, "未知表情"), "未知表情")
		action := "进行了"
		if kind == "MESSAGE_REACTION_REMOVE" {
			action = "移除了"
		}
		return fmt.Sprintf("频道 %s 的子频道 %s 的用户 %s 对 %s %s表态: %s", group, channelName, firstLogValue(userID, operatorID, "未知用户"), target, action, emoji)
	case "GROUP_MSG_REJECT", "GROUP_MSG_RECEIVE", "C2C_MSG_REJECT", "C2C_MSG_RECEIVE":
		target := "用户 " + person
		if strings.HasPrefix(kind, "GROUP_") {
			target = "群 " + group
		}
		action := "拒绝"
		if strings.HasSuffix(kind, "_RECEIVE") {
			action = "恢复"
		}
		return fmt.Sprintf("%s %s接收机器人消息。", target, action)
	case "INTERACTION_CREATE":
		if evt.Button != nil {
			return fmt.Sprintf("用户 %s 点击了机器人按钮 %s 。", person, firstLogValue(evt.Button.Id, "未知按钮"))
		}
		if evt.Argv != nil {
			return fmt.Sprintf("用户 %s 调用了机器人指令 %s 。", person, firstLogValue(evt.Argv.Name, "未知指令"))
		}
		return fmt.Sprintf("收到用户 %s 的互动事件。", person)
	case "MESSAGE_AUDIT_PASS":
		return fmt.Sprintf("消息审核通过，审核编号: %s。", firstLogValue(logValue(data, "audit_id"), "未知"))
	case "MESSAGE_AUDIT_REJECT":
		return fmt.Sprintf("消息审核未通过，审核编号: %s，原因: %s", firstLogValue(logValue(data, "audit_id"), "未知"), firstLogValue(logValue(data, "reject_reason"), "开放平台未提供拒绝原因"))
	default:
		return fmt.Sprintf("收到 QQ 开放平台事件: %s", firstLogValue(kind, "未知事件"))
	}
}

func channelLogType(kind string) string {
	switch kind {
	case "0":
		return "文字子频道"
	case "2":
		return "语音子频道"
	case "4":
		return "子频道分组"
	case "10005":
		return "直播子频道"
	case "10006":
		return "应用子频道"
	case "10007":
		return "论坛子频道"
	default:
		return "未知类型子频道"
	}
}
