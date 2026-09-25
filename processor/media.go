package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/glyccat/log"
	imageutil "github.com/WindowsSov8forUs/glyccat/pkg/image"
	"github.com/WindowsSov8forUs/glyccat/pkg/mp4"
	"github.com/WindowsSov8forUs/glyccat/pkg/silk"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

var mediaPreparing = make(chan struct{}, 2)

// prepareMessageMedia 恢复旧实现对内联媒体的必要转换，HTTP 资源仍原样交给 SDK。
// 新版内部资源只通过所属服务读取，不恢复远程 file:// 或裸文件路径访问。
func (a *Adapter) prepareMessageMedia(request *server.Request[server.MessageCreateParam]) (string, error) {
	content := request.Params.Content
	if request.Platform != "qq" && request.Platform != "qqguild" {
		return content, nil
	}
	if !strings.Contains(content, "data:") && !strings.Contains(content, "internal:") {
		return content, nil
	}
	ctx, cancel := context.WithTimeout(requestContext(request.Origin), 60*time.Second)
	defer cancel()
	stop := context.AfterFunc(a.closed, cancel)
	defer stop()
	if err := a.closed.Err(); err != nil {
		return "", err
	}
	select {
	case mediaPreparing <- struct{}{}:
		defer func() { <-mediaPreparing }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	tags, err := scanContentTags(ctx, content)
	if err != nil {
		return "", mediaError(err)
	}
	changes := make(map[int]string)
	inputBytes, outputSize, resources := 0, len(content), 0
	for index, tag := range tags {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if tag.skipResources {
			continue // 引用或平台扩展的说明内容不是本次要发送的资源。
		}
		kind := tag.token.Data
		if kind == "image" {
			kind = "img"
		}
		if kind != "img" && kind != "audio" && kind != "video" {
			continue
		}
		if request.Platform == "qqguild" {
			referrer := request.Params.Referrer.ValueOr(nil)
			direct, _ := referrer["direct"].(bool)
			if kind != "img" || direct || strings.Contains(request.Params.ChannelID, "_") {
				continue // 频道音视频和频道私信本地图片仍交给 SDK 返回原有不支持状态。
			}
		}
		src, ok := tagAttribute(tag.token, "src")
		if !ok || (!strings.HasPrefix(src, "data:") && !strings.HasPrefix(src, "internal:")) {
			continue
		}
		resources++
		if resources > 64 {
			return "", server.NewActionError(413, "单条消息的本地媒体数量超过 64 个", nil)
		}
		payload, err := convert.ResolveMessageResourcePayload(src)
		if err != nil {
			return "", server.NewActionError(400, "媒体资源地址无效: "+log.SafeText(err.Error()), err)
		}
		data := payload.Data
		if payload.Internal != "" {
			parts := strings.SplitN(strings.TrimPrefix(payload.Internal, "internal:"), "/", 3)
			if len(parts) != 3 || parts[0] != request.Platform || parts[1] != request.SelfID {
				return "", server.Forbidden("内部媒体资源不属于当前账号")
			}
			if a.resourceServer == nil {
				return "", server.NewActionError(503, "媒体所属服务尚未就绪", nil)
			}
			data, err = a.resourceServer.GetLocalFile(payload.Internal)
			if err != nil {
				return "", err
			}
		}
		if len(data) == 0 {
			return "", server.BadRequest("媒体资源为空")
		}
		if len(data) > maxContentBytes-inputBytes {
			return "", server.NewActionError(413, "本次消息的媒体输入总量超过 32 MiB", nil)
		}
		inputBytes += len(data)
		converted, contentType, changed, err := prepareMediaBytes(ctx, kind, data)
		if err != nil {
			return "", mediaError(err)
		}
		if !changed {
			continue // 已兼容的媒体不二次编码，保留动画、原始字节与资源地址。
		}
		prefix := "data:" + contentType + ";base64,"
		if base64.StdEncoding.EncodedLen(len(converted)) > maxContentBytes-len(prefix) {
			return "", server.NewActionError(413, "转换后媒体的内联编码超过消息上限", nil)
		}
		newSrc := prefix + base64.StdEncoding.EncodeToString(converted)
		updated, err := replaceTagAttribute(content[tag.start:tag.end], "src", newSrc)
		if err != nil {
			return "", server.BadRequest(err.Error())
		}
		if title, present := tagAttribute(tag.token, "title"); present && title != "" {
			extension := mediaExtension(contentType)
			if extension != "" {
				title = strings.TrimSuffix(title, path.Ext(title)) + extension
				updated, err = replaceTagAttribute(updated, "title", title)
				if err != nil {
					return "", server.BadRequest(err.Error())
				}
			}
		}
		outputSize += len(updated) - (tag.end - tag.start)
		if outputSize > maxContentBytes {
			return "", server.NewActionError(413, "媒体转换后的消息总量超过 32 MiB", nil)
		}
		changes[index] = updated
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	result, err := applyContentChanges(content, tags, changes)
	return result, mediaError(err)
}

func prepareMediaBytes(ctx context.Context, kind string, data []byte) ([]byte, string, bool, error) {
	switch kind {
	case "img":
		contentType, valid := imageutil.CheckImage(bytes.NewReader(data))
		if !valid {
			return nil, "", false, server.BadRequest("图片格式无效或尺寸超过本地限制")
		}
		if imageutil.IsGIForPNGorJPG(data) {
			return data, contentType, false, nil
		}
		converted, err := imageutil.EncoderImage(data)
		if err != nil {
			return nil, "", false, err
		}
		return converted, http.DetectContentType(converted), true, ctx.Err()
	case "audio":
		if silk.IsAMRorSILK(data) {
			return data, "", false, nil
		}
		converted, err := silk.EncoderSilkContext(ctx, data)
		return converted, "audio/silk", true, err
	case "video":
		// 延续旧版“非 MP4 才转换”的策略；容器匹配不保证任意编码均获平台接受。
		if mp4.IsMP4(data) {
			return data, "video/mp4", false, nil
		}
		converted, err := mp4.EncoderMP4Context(ctx, data)
		return converted, "video/mp4", true, err
	default:
		return nil, "", false, fmt.Errorf("不支持的媒体预处理类型")
	}
}

func mediaExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "audio/silk":
		return ".silk"
	case "video/mp4":
		return ".mp4"
	default:
		return ""
	}
}

func mediaError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var actionError server.SatoriError
	if errors.As(err, &actionError) {
		return err
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, exec.ErrDot) {
		return server.NewActionError(503, "媒体转换需要安装可信的 ffmpeg 并将其目录加入 PATH", err)
	}
	return server.NewActionError(422, "媒体预处理失败: "+err.Error(), err)
}
