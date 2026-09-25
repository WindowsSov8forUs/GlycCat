package silk

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/WindowsSov8forUs/glyccat/pkg/command"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

//go:embed exec/*
var silkCodecs embed.FS

const (
	HeaderAmr  string = "#!AMR"         // AMR 文件头
	HeaderSilk string = "\x02#!SILK_V3" // Silkv3 文件头
)

const maxAudioBytes = 32 * 1024 * 1024

var audioProcessing = make(chan struct{}, 2)

const limit = 4 * 1024

// IsAMRorSILK 判断是否是 AMR 或 SILK 文件
func IsAMRorSILK(file []byte) bool {
	return bytes.HasPrefix(file, []byte(HeaderAmr)) || bytes.HasPrefix(file, []byte(HeaderSilk))
}

// CheckAudio 判断给定音频流是否为合法音频
func CheckAudio(readSeeker io.ReadSeeker) (string, bool) {
	t := scanType(readSeeker)
	if strings.Contains(t, "audio") {
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

// EncoderSilk 编码为 SILK
func EncoderSilk(data []byte) ([]byte, error) {
	return EncoderSilkContext(context.Background(), data)
}

// EncoderSilkContext 按请求取消转码，两步转换共享时间和独立目录
func EncoderSilkContext(ctx context.Context, data []byte) (result []byte, resultErr error) {
	if len(data) == 0 || len(data) > maxAudioBytes {
		return nil, fmt.Errorf("音频内容为空或超过 32 MiB 上限")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case audioProcessing <- struct{}{}:
		defer func() { <-audioProcessing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	codecName, err := getSilkCodecPath()
	if err != nil {
		return nil, err
	}
	codecData, err := silkCodecs.ReadFile(codecName)
	if err != nil {
		return nil, fmt.Errorf("读取内嵌 SILK 编码器失败: %w", err)
	}
	program, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("未找到可信的 ffmpeg，请将安装目录加入 PATH: %w", err)
	}
	dir, err := os.MkdirTemp("", "glyccat-audio-*")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(dir)) }()
	input := filepath.Join(dir, "input")
	pcm := filepath.Join(dir, "output.pcm")
	output := filepath.Join(dir, "output.silk")
	codec := filepath.Join(dir, "codec")
	if runtime.GOOS == "windows" {
		codec += ".exe"
	}
	if err := os.WriteFile(input, data, 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(codec, codecData, 0700); err != nil {
		return nil, err
	}
	sampleRate := 24000
	cmd := exec.CommandContext(ctx, program,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-max_alloc", "67108864",
		"-protocol_whitelist", "file,pipe",
		"-format_whitelist", "aac,ac3,aiff,amr,ape,flac,matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mp3,ogg,wav",
		"-threads", "2", "-i", input, "-map", "0:a:0", "-threads", "2",
		"-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1", "-fs", strconv.Itoa(maxAudioBytes), pcm)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	if err := command.Run(ctx, cmd, "音频转换为 PCM"); err != nil {
		return nil, err
	}
	info, err := os.Stat(pcm)
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() >= maxAudioBytes {
		return nil, fmt.Errorf("PCM 内容为空或达到输出上限，不发送截断音频")
	}
	args := []string{"-i", pcm, "-o", output, "-s", strconv.Itoa(sampleRate)}
	if runtime.GOOS != "windows" {
		args = append([]string{"pts"}, args...)
	}
	cmd = exec.CommandContext(ctx, codec, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	if err := command.Run(ctx, cmd, "SILK 编码"); err != nil {
		return nil, err
	}
	file, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result, err = io.ReadAll(io.LimitReader(file, maxAudioBytes+1))
	if err != nil {
		return nil, err
	}
	if len(result) > maxAudioBytes || !IsAMRorSILK(result) {
		return nil, fmt.Errorf("SILK 编码结果格式无效或超过上限")
	}
	return result, ctx.Err()
}

// getSilkCodecPath 获取 SILK 编解码器路径
func getSilkCodecPath() (string, error) {
	var codecFileName string
	// 根据 OS 不同获取不同路径
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH != "amd64" {
			return "", fmt.Errorf("当前 Windows 架构没有匹配的 SILK 编码器: %s", runtime.GOARCH)
		}
		codecFileName = "silk_codec-windows.exe"
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			codecFileName = "silk_codec-linux-x64"
		case "arm64":
			codecFileName = "silk_codec-linux-arm64"
		default:
			return "", fmt.Errorf("当前 Linux 架构没有匹配的 SILK 编码器: %s", runtime.GOARCH)
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			codecFileName = "silk_codec-macos"
		default:
			return "", fmt.Errorf("当前 %s/%s 没有匹配的 SILK 编码器", runtime.GOOS, runtime.GOARCH)
		}
	case "android":
		switch runtime.GOARCH {
		case "arm64":
			codecFileName = "silk_codec-android-arm64"
		case "386":
			codecFileName = "silk_codec-android-x86"
		case "amd64":
			codecFileName = "silk_codec-android-x86_64"
		default:
			return "", fmt.Errorf("当前 %s/%s 没有匹配的 SILK 编码器", runtime.GOOS, runtime.GOARCH)
		}
	default:
		return "", fmt.Errorf("当前平台没有匹配的 SILK 编码器: %s", runtime.GOOS)
	}

	return "exec/" + codecFileName, nil
}
