package image

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	HeaderJPG  = "\xFF\xD8"
	HeaderPNG  = "\x89PNG\r\n\x1a\n"
	HeaderGIF  = "GIF87a"
	HeaderGIF2 = "GIF89a"

	maxImageBytes  = 32 * 1024 * 1024
	maxImagePixels = 16 * 1024 * 1024
)

// IsGIForPNGorJPG 判断是否为 GIF/PNG/JPG
func IsGIForPNGorJPG(data []byte) bool {
	return bytes.HasPrefix(data, []byte(HeaderJPG)) || bytes.HasPrefix(data, []byte(HeaderPNG)) ||
		bytes.HasPrefix(data, []byte(HeaderGIF)) || bytes.HasPrefix(data, []byte(HeaderGIF2))
}

// CheckImage 在完整解码前检查图片尺寸，并恢复读取位置
func CheckImage(reader io.ReadSeeker) (string, bool) {
	if reader == nil {
		return "", false
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return "", false
	}
	defer reader.Seek(0, io.SeekStart)
	config, format, err := image.DecodeConfig(io.LimitReader(reader, maxImageBytes+1))
	if err != nil || !validImageSize(config) {
		return "", false
	}
	if format == "jpeg" {
		return "image/jpeg", true
	}
	return "image/" + format, true
}

// EncoderImage 重编码图像，不使用共享临时文件，先限制压缩大小和像素数量
func EncoderImage(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxImageBytes {
		return nil, fmt.Errorf("图片内容为空或超过 32 MiB 上限")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("读取图片格式失败: %w", err)
	}
	if !validImageSize(config) {
		return nil, fmt.Errorf("图片尺寸超过 8192 边长或 1600 万像素限制")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	buffer := new(imageBuffer)
	if format == "bmp" {
		err = jpeg.Encode(buffer, img, nil)
	} else {
		err = png.Encode(buffer, img)
	}
	if err != nil {
		return nil, fmt.Errorf("图片重编码失败: %w", err)
	}
	return buffer.Bytes(), nil
}

func validImageSize(config image.Config) bool {
	return config.Width > 0 && config.Height > 0 && config.Width <= 8192 && config.Height <= 8192 &&
		int64(config.Width)*int64(config.Height) <= maxImagePixels
}

type imageBuffer struct {
	bytes.Buffer
}

func (b *imageBuffer) Write(data []byte) (int, error) {
	if len(data) > maxImageBytes-b.Len() {
		return 0, fmt.Errorf("重编码图片超过 32 MiB 上限")
	}
	return b.Buffer.Write(data)
}
