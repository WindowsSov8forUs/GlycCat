package mp4

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/WindowsSov8forUs/glyccat/pkg/command"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxVideoBytes = 32 * 1024 * 1024

var videoProcessing = make(chan struct{}, 2)

const limit = 4 * 1024

var HeadersMP4 []string = []string{
	"ftypisom",
	"ftypmp42",
}

// IsMP4 判断是否为 MP4 文件
func IsMP4(file []byte) bool {
	// ISO BMFF 的 ftyp 前面是 box 长度，不能从第一个字节比较品牌名。
	if len(file) < 16 || !bytes.Equal(file[4:8], []byte("ftyp")) {
		return false
	}
	size := uint64(binary.BigEndian.Uint32(file[:4]))
	headerSize := 8
	if size == 1 {
		if len(file) < 24 {
			return false
		}
		size = binary.BigEndian.Uint64(file[8:16])
		headerSize = 16
	}
	if size < uint64(headerSize+8) || size > uint64(len(file)) || (size-uint64(headerSize))%4 != 0 {
		return false
	}
	for offset := headerSize; offset+4 <= int(size); offset += 4 {
		if offset == headerSize+4 {
			continue // minor_version 不是品牌名。
		}
		brand := string(file[offset : offset+4])
		switch brand {
		case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "avc1", "mp41", "mp42":
			return true
		}
	}
	return false
}

// CheckVideo 判断给定视频流是否为合法视频
func CheckVideo(readSeeker io.ReadSeeker) (string, bool) {
	t := scanType(readSeeker)
	if strings.Contains(t, "video") {
		return t, true
	}
	return t, false
}

// scanType 扫描格式
func scanType(reader io.ReadSeeker) string {
	if reader == nil {
		return ""
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	defer reader.Seek(0, io.SeekStart)
	data := make([]byte, limit)
	n, err := io.ReadFull(reader, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ""
	}
	return http.DetectContentType(data[:n])
}

// EncoderMP4 编码为 MP4
func EncoderMP4(data []byte) ([]byte, error) {
	return EncoderMP4Context(context.Background(), data)
}

// EncoderMP4Context 使用独立目录和请求上下文转码，避免冲突及无限等待
func EncoderMP4Context(ctx context.Context, data []byte) (result []byte, resultErr error) {
	if len(data) == 0 || len(data) > maxVideoBytes {
		return nil, fmt.Errorf("视频内容为空或超过 32 MiB 上限")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case videoProcessing <- struct{}{}:
		defer func() { <-videoProcessing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	program, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("未找到可信的 ffmpeg，请将安装目录加入 PATH: %w", err)
	}
	dir, err := os.MkdirTemp("", "glyccat-video-*")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(dir)) }()
	input := filepath.Join(dir, "input")
	output := filepath.Join(dir, "output.mp4")
	if err := os.WriteFile(input, data, 0600); err != nil {
		return nil, err
	}
	// 不允许网络协议、HLS 或 concat 等外部资源引用型输入。
	cmd := exec.CommandContext(ctx, program,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-max_alloc", "67108864",
		"-protocol_whitelist", "file,pipe",
		"-format_whitelist", "avi,asf,flv,matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpeg,mpegts,ogg",
		"-threads", "2", "-i", input, "-map", "0:v:0", "-map", "0:a:0?",
		"-threads", "2", "-filter_threads", "1", "-vcodec", "libx264", "-pix_fmt", "yuv420p",
		"-acodec", "aac", "-movflags", "+faststart", "-fs", strconv.Itoa(maxVideoBytes), output)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	if err := command.Run(ctx, cmd, "视频转码"); err != nil {
		return nil, err
	}
	file, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result, err = io.ReadAll(io.LimitReader(file, maxVideoBytes+1))
	if err != nil {
		return nil, err
	}
	if len(result) >= maxVideoBytes {
		return nil, fmt.Errorf("视频转码达到输出上限，不发送可能被截断的内容")
	}
	if !IsMP4(result) {
		return nil, fmt.Errorf("视频转码未得到有效的 MP4 文件")
	}
	return result, ctx.Err()
}
