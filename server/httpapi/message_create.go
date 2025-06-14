package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/WindowsSov8forUs/glyccat/fileserver"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/WindowsSov8forUs/glyccat/processor"
	"github.com/gin-gonic/gin"

	"github.com/satori-protocol-go/satori-model-go/pkg/channel"
	"github.com/satori-protocol-go/satori-model-go/pkg/guild"
	"github.com/satori-protocol-go/satori-model-go/pkg/guildmember"
	satoriMessage "github.com/satori-protocol-go/satori-model-go/pkg/message"
	"github.com/satori-protocol-go/satori-model-go/pkg/user"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/dto/keyboard"
	"github.com/tencent-connect/botgo/openapi"
)

func init() {
	RegisterHandler("message.create", HandleMessageCreate)
}

// RequestMessageCreate 发送消息请求
type RequestMessageCreate struct {
	ChannelId string `json:"channel_id"` // 频道 ID
	Content   string `json:"content"`    // 消息内容
}

// ResponseMessageCreate 发送消息响应
type ResponseMessageCreate []satoriMessage.Message

// HandleMessageCreate 处理发送消息请求
func HandleMessageCreate(api, apiv2 openapi.OpenAPI, message *ActionMessage) (any, APIError) {
	var request RequestMessageCreate
	err := json.Unmarshal(message.Data(), &request)
	if err != nil {
		return gin.H{}, &BadRequestError{err}
	}

	if message.Platform == "qqguild" {
		var response ResponseMessageCreate

		// 尝试获取私聊频道，若没有获取则视为群组频道
		guildId := processor.GetDirectChannelGuild(request.ChannelId)
		if guildId == "" {
			// 输出日志
			log.Infof("发送消息到频道 %s : %s", request.ChannelId, logContent(request.Content))

			var dtoMessageToCreate = &dto.MessageToCreate{}
			dtoMessageToCreate, err = convertToMessageToCreate(request.Content, message.Bot.Id, true)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			var dtoMessage *dto.Message
			dtoMessage, err = api.PostMessage(context.TODO(), request.ChannelId, dtoMessageToCreate)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			messageResponse, err := convertDtoMessageToMessage(dtoMessage)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			response = append(response, *messageResponse)
		} else {
			// 输出日志
			log.Infof("发送消息到私聊频道 %s : %s", request.ChannelId, logContent(request.Content))

			var dtoMessageToCreate = &dto.MessageToCreate{}
			var dtoDirectMessage = &dto.DirectMessage{}
			dtoMessageToCreate, err = convertToMessageToCreate(request.Content, message.Bot.Id, false)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			dtoDirectMessage.ChannelID = request.ChannelId
			dtoDirectMessage.GuildID = guildId
			var dtoMessage *dto.Message
			dtoMessage, err = api.PostDirectMessage(context.TODO(), dtoDirectMessage, dtoMessageToCreate)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			messageResponse, err := convertDtoMessageToMessage(dtoMessage)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			response = append(response, *messageResponse)
		}

		return response, nil
	} else if message.Platform == "qq" {
		var response ResponseMessageCreate

		// 尝试获取消息类型
		openIdType := processor.GetOpenIdType(request.ChannelId)
		if openIdType == "private" {
			// 输出日志
			log.Infof("发送消息到用户 %s : %s", request.ChannelId, logContent(request.Content))

			// 是私聊频道
			var dtoMessageToCreate = &dto.MessageToCreate{}
			dtoMessageToCreate, err = convertToMessageToCreateV2(request.Content, request.ChannelId, openIdType, apiv2)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			var dtoC2CMessageResponse *dto.C2CMessageResponse
			dtoC2CMessageResponse, err = api.PostC2CMessage(context.TODO(), request.ChannelId, dtoMessageToCreate)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			messageResponse, err := convertDtoMessageV2ToMessage(dtoC2CMessageResponse.Message)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			response = append(response, *messageResponse)
		} else {
			// 是群聊频道
			openIdType = "group"

			// 输出日志
			log.Infof("发送消息到群 %s : %s", request.ChannelId, logContent(request.Content))

			var dtoMessageToCreate = &dto.MessageToCreate{}
			dtoMessageToCreate, err = convertToMessageToCreateV2(request.Content, request.ChannelId, openIdType, apiv2)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			var dtoGroupMessageResponse *dto.GroupMessageResponse
			dtoGroupMessageResponse, err = api.PostGroupMessage(context.TODO(), request.ChannelId, dtoMessageToCreate)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			messageResponse, err := convertDtoMessageV2ToMessage(dtoGroupMessageResponse.Message)
			if err != nil {
				return gin.H{}, &InternalServerError{err}
			}
			response = append(response, *messageResponse)
		}

		return response, nil
	}

	return defaultResource(message)
}

// logContent 将内容处理为输出内容
func logContent(content string) string {
	if len(content) > 50 {
		return content[:40] + "..." + content[len(content)-10:]
	} else {
		return content
	}
}

// convertToMessageToCreate 转换为消息体结构
func convertToMessageToCreate(content, userId string, isGuild bool) (*dto.MessageToCreate, error) {
	// 将文本消息内容转换为 satoriMessage.MessageElement
	elements, err := satoriMessage.Parse(content)
	if err != nil {
		return nil, err
	}

	// 处理 satoriMessage.MessageElement
	var dtoMessageToCreate = &dto.MessageToCreate{}
	err = parseElementsInMessageToCreate(elements, dtoMessageToCreate, isGuild, userId)
	if err != nil {
		return nil, err
	}
	return dtoMessageToCreate, nil
}

// parseElementsInMessageToCreate 将 Satori 消息元素转换为消息体结构
func parseElementsInMessageToCreate(elements []satoriMessage.MessageElement, dtoMessageToCreate *dto.MessageToCreate, isGuild bool, userId string) error {
	// 处理 satoriMessage.MessageElement
	for _, element := range elements {
		// 根据元素类型进行处理
		switch e := element.(type) {
		case *satoriMessage.MessageElementText:
			dtoMessageToCreate.Content += e.Content
		case *satoriMessage.MessageElementAt:
			if isGuild {
				if e.Type == "all" {
					dtoMessageToCreate.Content += "@everyone"
				} else {
					if e.Id != "" {
						dtoMessageToCreate.Content += fmt.Sprintf("<@%s>", e.Id)
					} else {
						continue
					}
				}
			}
		case *satoriMessage.MessageElementSharp:
			dtoMessageToCreate.Content += fmt.Sprintf("<#%s>", e.Id)
		case *satoriMessage.MessageElementA:
			dtoMessageToCreate.Content += e.Href
		case *satoriMessage.MessageElementImg:
			if dtoMessageToCreate.Image != "" {
				// 只支持发一张图片
				// TODO: 多图片时分割发送
				continue
			}

			url, file, err := processor.ParseSrc(e.Src)
			if err != nil {
				log.Warnf("解析图片 src 失败: %s", err)
				continue
			}

			// 如果是 URL 则直接放入
			if url != "" {
				dtoMessageToCreate.Image = url
			} else if file != nil {
				// 保存至文件服务器并放入资源链接
				fileReader, err := file.GetReader()
				if err != nil {
					log.Warnf("获取文件读取器失败: %s", err)
					continue
				}

				// 生成资源标识
				ident, err := fileserver.CalculateFileIdent("qqguild", userId, fileReader)
				if err != nil {
					log.Warnf("计算文件标识失败: %s", err)
					continue
				}

				// 尝试获取文件
				if e.Cache {
					meta, err := fileserver.GetFile(ident)
					if err == nil {
						dtoMessageToCreate.Image = fileserver.InternalURL(meta)
						continue
					}
				}

				// 保存至本地文件服务器
				fileReader, err = file.GetReader()
				if err != nil {
					log.Warnf("获取文件读取器失败: %s", err)
					continue
				}
				meta, err := fileserver.SaveFile(fileReader, "qqguild", userId, e.Title, file.MimeType)
				if err != nil {
					log.Warnf("保存图片文件失败: %s", err)
					continue
				}
				dtoMessageToCreate.Image = fileserver.InternalURL(meta)
			} else {
				log.Warnf("图片元素没有有效的 src 或文件")
			}
		case *satoriMessage.MessageElementAudio:
			// 频道不支持音频消息
			continue
		case *satoriMessage.MessageElementVideo:
			// TODO: 频道支持视频消息，但是并未找到支持的实现
			continue
		case *satoriMessage.MessageElementFile:
			// 频道不支持文件消息
			continue
		// TODO: 修饰元素全部视为子元素集合，或许可以变成 dto.markdown ？
		case *satoriMessage.MessageElementStrong:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementEm:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementIns:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementDel:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementSpl:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementCode:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementSup:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementSub:
			// 递归调用
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElmentBr:
			dtoMessageToCreate.Content += "\n"
		case *satoriMessage.MessageElmentP:
			dtoMessageToCreate.Content += "\n"
			// 视为子元素集合
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
			dtoMessageToCreate.Content += "\n"
		case *satoriMessage.MessageElementMessage:
			// 视为子元素集合，目前不支持视为转发消息
			parseElementsInMessageToCreate(e.GetChildren(), dtoMessageToCreate, isGuild, userId)
		case *satoriMessage.MessageElementQuote:
			// 遍历子元素，只会处理第一个 satoriMessage.MessageElementMessage 元素
			for _, child := range e.GetChildren() {
				if m, ok := child.(*satoriMessage.MessageElementMessage); ok {
					if m.Id != "" {
						dtoMessageReference := &dto.MessageReference{
							MessageID: m.Id,
						}
						dtoMessageToCreate.MessageReference = dtoMessageReference
						break
					}
				}
			}
		case *satoriMessage.MessageElementButton:
			// TODO: 频道的按钮怎么处理？
			continue
		case *satoriMessage.MessageElementExtend:
			// 从扩展消息中选取有用的消息
			switch e.Tag() {
			case "qq:passive":
				// 被动元素处理，作为消息发送的基础
				if id, ok := e.Get("id"); ok {
					dtoMessageToCreate.MsgID = id
				}
				if seq, ok := e.Get("seq"); ok {
					if intSeq, err := strconv.Atoi(seq); err == nil {
						dtoMessageToCreate.MsgSeq = intSeq
					}
				}
			}
		default:
			continue
		}
	}
	return nil
}

// convertToMessageToCreateV2 转换为 V2 消息体结构
func convertToMessageToCreateV2(content string, openId string, messageType string, apiv2 openapi.OpenAPI) (*dto.MessageToCreate, error) {
	// 将文本消息内容转换为 satoriMessage.MessageElement
	elements, err := satoriMessage.Parse(content)
	if err != nil {
		return nil, err
	}

	// 处理 satoriMessage.MessageElement
	var dtoMessageToCreate = &dto.MessageToCreate{}
	err = parseElementsInMessageToCreateV2(elements, dtoMessageToCreate, openId, messageType, apiv2)
	if err != nil {
		return nil, err
	}

	return dtoMessageToCreate, nil
}

// parseElementsInMessageToCreateV2 将 Satori 消息元素转换为 V2 消息体结构
func parseElementsInMessageToCreateV2(elements []satoriMessage.MessageElement, dtoMessageToCreate *dto.MessageToCreate, openId, messageType string, apiv2 openapi.OpenAPI) error {
	// 处理 satoriMessage.MessageElement
	for _, element := range elements {
		// 根据元素类型进行处理
		switch e := element.(type) {
		case *satoriMessage.MessageElementText:
			dtoMessageToCreate.Content += e.Content
		case *satoriMessage.MessageElementAt:
			// 单聊并不支持
			if messageType == "group" {
				if e.Type == "all" {
					// 只在文字子频道中可用
					continue
				} else {
					if e.Id != "" {
						dtoMessageToCreate.Content += fmt.Sprintf("<@%s>", e.Id)
					} else {
						continue
					}
				}
			}
		case *satoriMessage.MessageElementSharp:
			// 群聊/单聊并不支持
			continue
		case *satoriMessage.MessageElementA:
			dtoMessageToCreate.Content += e.Href
		case *satoriMessage.MessageElementImg:
			if dtoMessageToCreate.Media.FileInfo != "" {
				// 富媒体信息只支持一个
				continue
			}

			if err := parseResourceElementInMTCV2(e, dtoMessageToCreate, openId, messageType, apiv2); err != nil {
				return err
			}
		case *satoriMessage.MessageElementAudio:
			if dtoMessageToCreate.Media.FileInfo != "" {
				// 富媒体信息只支持一个
				continue
			}

			if err := parseResourceElementInMTCV2(e, dtoMessageToCreate, openId, messageType, apiv2); err != nil {
				return err
			}
		case *satoriMessage.MessageElementVideo:
			if dtoMessageToCreate.Media.FileInfo != "" {
				// 富媒体信息只支持一个
				continue
			}

			if err := parseResourceElementInMTCV2(e, dtoMessageToCreate, openId, messageType, apiv2); err != nil {
				return err
			}
		case *satoriMessage.MessageElementFile:
			// TODO: 本地缓冲
			if dtoMessageToCreate.Media.FileInfo != "" {
				// 富媒体信息只支持一个
				continue
			}

			if err := parseResourceElementInMTCV2(e, dtoMessageToCreate, openId, messageType, apiv2); err != nil {
				return err
			}
		// 修饰元素全部视为子元素集合，Markdown 是别想了
		case *satoriMessage.MessageElementStrong:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementEm:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementIns:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementDel:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementSpl:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementCode:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementSup:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementSub:
			// 递归调用
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElmentBr:
			dtoMessageToCreate.Content += "\n"
		case *satoriMessage.MessageElmentP:
			dtoMessageToCreate.Content += "\n"
			// 视为子元素集合
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
			dtoMessageToCreate.Content += "\n"
		case *satoriMessage.MessageElementMessage:
			// 视为子元素集合，目前不支持视为转发消息
			parseElementsInMessageToCreateV2(e.GetChildren(), dtoMessageToCreate, openId, messageType, apiv2)
		case *satoriMessage.MessageElementQuote:
			// 遍历子元素，只会处理第一个 satoriMessage.MessageElementMessage 元素
			for _, child := range e.GetChildren() {
				if m, ok := child.(*satoriMessage.MessageElementMessage); ok {
					if m.Id != "" {
						dtoMessageReference := &dto.MessageReference{
							MessageID: m.Id,
						}
						dtoMessageToCreate.MessageReference = dtoMessageReference
						break
					}
				}
			}
		case *satoriMessage.MessageElementButton:
			// TODO: 放弃了，不想管了
			dtoMessageToCreate.Keyboard = convertButtonToKeyboard(e)
		case *satoriMessage.MessageElementExtend:
			// 从扩展消息中选取有用的消息
			switch e.Tag() {
			case "qq:passive":
				// 被动元素处理，作为消息发送的基础
				if id, ok := e.Get("id"); ok {
					dtoMessageToCreate.MsgID = id
				}
				if seq, ok := e.Get("seq"); ok {
					if intSeq, err := strconv.Atoi(seq); err == nil {
						dtoMessageToCreate.MsgSeq = intSeq
					}
				}
			}
		default:
			continue
		}
	}
	return nil
}

// parseResourceElementInMTCV2 将 Satori 资源消息元素解析到 V2 消息体结构中
func parseResourceElementInMTCV2(element satoriMessage.MessageElement, dtoMessageToCreate *dto.MessageToCreate, openId, messageType string, apiv2 openapi.OpenAPI) error {
	// TODO: 这里似乎应该将所有资源元素统一到一个子类型中，然后再细分
	// TODO: 再说吧，需要改 satori-model-go 了

	var cache bool
	var src string

	targetId := fmt.Sprintf("%s:%s", messageType, openId)

	switch e := element.(type) {
	case *satoriMessage.MessageElementImg:
		cache = e.Cache
		src = e.Src
	case *satoriMessage.MessageElementAudio:
		cache = e.Cache
		src = e.Src
	case *satoriMessage.MessageElementVideo:
		cache = e.Cache
		src = e.Src
	case *satoriMessage.MessageElementFile:
		cache = e.Cache
		src = e.Src
	default:
		log.Warnf("消息元素类型不匹配: %T", element)
		return nil
	}

	// 生成资源标识
	srcId, err := processor.ParseSrcToString(src)
	if err != nil {
		log.Warnf("解析图片 src 失败: %s", err)
		return nil
	}
	ident, err := fileserver.CalculateFileInfoIdent(targetId, srcId)
	if err != nil {
		log.Warnf("计算文件信息标识失败: %s", err)
		return nil
	}

	if cache {
		info, err := fileserver.GetFileInfo(ident)
		if err == nil {
			dtoMessageToCreate.Media.FileInfo = info.FileInfo
			dtoMessageToCreate.MsgType = 7
			return nil
		}
	}

	// 生成上传用富媒体结构
	dtoRichMediaMessage, err := generateDtoRichMediaMessage(dtoMessageToCreate.MsgID, element)
	if err != nil {
		log.Warnf("生成富媒体消息失败: %s", err)
		return nil
	}

	// 上传富媒体
	var mediaResponse *dto.MediaResponse
	if messageType == "private" {
		mediaResponse, err = uploadMediaPrivate(context.TODO(), openId, dtoRichMediaMessage, apiv2)
		if err != nil {
			return err
		}
	} else {
		mediaResponse, err = uploadMedia(context.TODO(), openId, dtoRichMediaMessage, apiv2)
		if err != nil {
			return err
		}
	}

	// 保存富媒体
	if cache {
		// mediaResponse 的 TTL 以秒为单位，转换为 uint64 TTL
		ttl := uint64(mediaResponse.TTL) * 1000 // 转换为毫秒
		_, err := fileserver.SaveFileInfo(targetId, srcId, mediaResponse.FileInfo, ttl)
		if err != nil {
			log.Warnf("保存文件信息失败: %s", err)
		}
	}

	dtoMessageToCreate.Media.FileInfo = mediaResponse.FileInfo
	dtoMessageToCreate.MsgType = 7

	return nil
}

// convertDtoMessageToMessage 将收到的消息响应转化为符合 Satori 协议的消息
func convertDtoMessageToMessage(dtoMessage *dto.Message) (*satoriMessage.Message, error) {
	var message satoriMessage.Message

	message.Id = dtoMessage.ID
	message.Content = strings.TrimSpace(processor.ConvertToMessageContent(dtoMessage))

	// 判断消息类型
	if dtoMessage.ChannelID != "" {
		// 判断是否为私聊频道
		guildId := processor.GetDirectChannelGuild(dtoMessage.ChannelID)
		var channelType channel.ChannelType
		if guildId == "" {
			// 不是私聊频道
			channelType = channel.ChannelTypeText
		} else {
			// 是私聊频道
			channelType = channel.ChannelTypeDirect
		}
		channel := &channel.Channel{
			Id:   dtoMessage.ChannelID,
			Type: channelType,
		}
		guild := &guild.Guild{
			Id: dtoMessage.GuildID,
		}
		var guildMember *guildmember.GuildMember
		if dtoMessage.Member != nil {
			guildMember = &guildmember.GuildMember{}
			if dtoMessage.Member != nil {
				guildMember.Nick = dtoMessage.Member.Nick
			}
			if dtoMessage.Author != nil {
				guildMember.Avatar = dtoMessage.Author.Avatar
			}
		}
		user := &user.User{
			Id:     dtoMessage.Author.ID,
			Name:   dtoMessage.Author.Username,
			Avatar: dtoMessage.Author.Avatar,
			IsBot:  dtoMessage.Author.Bot,
		}

		// 获取时间
		if dtoMessage.Member != nil {
			time, err := dtoMessage.Member.JoinedAt.Time()
			if err != nil {
				return nil, err
			}
			guildMember.JoinedAt = time.UnixMilli()
		}

		message.Channel = channel
		message.Guild = guild
		message.Member = guildMember
		message.User = user
	} else {
		// 判断是否为单聊
		if dtoMessage.GroupID != "" {
			// 是群聊
			channel := &channel.Channel{
				Id:   dtoMessage.GroupID,
				Type: channel.ChannelTypeText,
			}
			guild := &guild.Guild{
				Id: dtoMessage.GroupID,
			}
			var guildMember *guildmember.GuildMember
			if dtoMessage.Member == nil {
				guildMember = &guildmember.GuildMember{}
				if dtoMessage.Member != nil {
					guildMember.Nick = dtoMessage.Member.Nick
				}
				if dtoMessage.Author != nil {
					guildMember.Avatar = dtoMessage.Author.Avatar
				}
			}
			user := &user.User{
				Id:     dtoMessage.Author.ID,
				Name:   dtoMessage.Author.Username,
				Avatar: dtoMessage.Author.Avatar,
				IsBot:  dtoMessage.Author.Bot,
			}

			// 获取时间
			if dtoMessage.Member != nil {
				time, err := dtoMessage.Member.JoinedAt.Time()
				if err != nil {
					return nil, err
				}
				guildMember.JoinedAt = time.UnixMilli()
			}

			message.Channel = channel
			message.Guild = guild
			message.Member = guildMember
			message.User = user
		} else {
			// 是单聊
			channel := &channel.Channel{
				Id:   dtoMessage.Author.ID,
				Type: channel.ChannelTypeDirect,
			}
			user := &user.User{
				Id:     dtoMessage.Author.ID,
				Name:   dtoMessage.Author.Username,
				Avatar: dtoMessage.Author.Avatar,
				IsBot:  dtoMessage.Author.Bot,
			}
			message.Channel = channel
			message.User = user
		}
	}

	time, err := dtoMessage.Timestamp.Time()
	if err != nil {
		return nil, err
	}
	message.CreateAt = time.UnixMilli()

	return &message, nil
}

// convertDtoMessageV2ToMessage 将收到的 V2 消息响应转化为符合 Satori 协议的消息
func convertDtoMessageV2ToMessage(dtoMessage *dto.Message) (*satoriMessage.Message, error) {
	var message satoriMessage.Message

	message.Id = dtoMessage.ID
	if content := strings.TrimSpace(processor.ConvertToMessageContent(dtoMessage)); content != "" {
		message.Content = content
	}

	time, err := dtoMessage.Timestamp.Time()
	if err != nil {
		return nil, err
	}
	message.CreateAt = time.UnixMilli()

	return &message, nil
}

// convertButtonToKeyboard 将 Satori 协议的按钮转换为 QQ 的按钮
func convertButtonToKeyboard(button *satoriMessage.MessageElementButton) *keyboard.MessageKeyboard {
	// 目前官方 Bot 不再新增支持除指定模板 ID 以外的所有形式

	var messageKeyboard keyboard.MessageKeyboard

	messageKeyboard.ID = button.Id

	return &messageKeyboard
}

// uploadMedia 上传媒体并返回FileInfo
func uploadMedia(ctx context.Context, groupID string, richMediaMessage *dto.RichMediaMessage, apiv2 openapi.OpenAPI) (*dto.MediaResponse, error) {
	// 调用API来上传媒体
	messageReturn, err := apiv2.PostGroupMessage(ctx, groupID, richMediaMessage)
	if err != nil {
		return nil, err
	}

	// 返回上传后的FileInfo
	return messageReturn.MediaResponse, nil
}

// uploadMedia 上传媒体并返回FileInfo
func uploadMediaPrivate(ctx context.Context, userID string, richMediaMessage *dto.RichMediaMessage, apiv2 openapi.OpenAPI) (*dto.MediaResponse, error) {
	// 调用API来上传媒体
	messageReturn, err := apiv2.PostC2CMessage(ctx, userID, richMediaMessage)
	if err != nil {
		return nil, err
	}

	// 返回上传后的FileInfo
	return messageReturn.MediaResponse, nil
}

// generateDtoRichMediaMessage 创建 dto.RichMediaMessage
func generateDtoRichMediaMessage(id string, element satoriMessage.MessageElement) (*dto.RichMediaMessage, error) {
	var dtoRichMediaMessage *dto.RichMediaMessage

	// 根据 element 的类型来创建 dto.RichMediaMessage
	switch e := element.(type) {
	case *satoriMessage.MessageElementImg:
		url, fileData, err := processor.ParseSrcToAvailavle(e.Src)
		if err != nil {
			return nil, err
		}
		dtoRichMediaMessage = &dto.RichMediaMessage{
			EventID:    id,
			FileType:   1,
			URL:        url,
			FileData:   fileData,
			SrvSendMsg: false,
		}
	case *satoriMessage.MessageElementVideo:
		url, fileData, err := processor.ParseSrcToAvailavle(e.Src)
		if err != nil {
			return nil, err
		}
		dtoRichMediaMessage = &dto.RichMediaMessage{
			EventID:    id,
			FileType:   2,
			URL:        url,
			FileData:   fileData,
			SrvSendMsg: false,
		}
	case *satoriMessage.MessageElementAudio:
		url, fileData, err := processor.ParseSrcToAvailavle(e.Src)
		if err != nil {
			return nil, err
		}
		dtoRichMediaMessage = &dto.RichMediaMessage{
			EventID:    id,
			FileType:   3,
			URL:        url,
			FileData:   fileData,
			SrvSendMsg: false,
		}
	case *satoriMessage.MessageElementFile:
		url, fileData, err := processor.ParseSrcToAvailavle(e.Src)
		if err != nil {
			return nil, err
		}
		dtoRichMediaMessage = &dto.RichMediaMessage{
			EventID:    id,
			FileType:   4,
			URL:        url,
			FileData:   fileData,
			SrvSendMsg: false,
		}
	default:
		return nil, fmt.Errorf("不支持的消息元素类型: %T", element)
	}

	return dtoRichMediaMessage, nil
}
