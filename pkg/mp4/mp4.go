package mp4

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

const cachePath = "data/cache"

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
func scanType(readerSeeker io.ReadSeeker) string {
	_, _ = readerSeeker.Seek(0, io.SeekStart)
	defer readerSeeker.Seek(0, io.SeekStart)
	in := make([]byte, limit)
	_, _ = readerSeeker.Read(in)
	return http.DetectContentType(in)
}

// EncoderMP4 编码为 MP4
func EncoderMP4(data []byte) ([]byte, error) {
	hash := md5.New()
	_, err := hash.Write(data)
	if err != nil {
		return nil, fmt.Errorf("failed to compute md5: %v", err)
	}
	name := hex.EncodeToString(hash.Sum(nil))
	return encode(data, name)
}

// encode 编码为 MP4
func encode(data []byte, name string) (mp4Video []byte, err error) {
	// 0. 创建缓存目录
	err = createDirectoryIfNotExist(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create video cache directory: %v", err)
	}

	// 1. 创建临时文件
	rawPath := path.Join(cachePath, name)
	err = os.WriteFile(rawPath, data, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer os.Remove(rawPath)

	// 2. 转换 MP4
	mp4Path := path.Join(cachePath, name+".mp4")
	cmd := exec.Command("ffmpeg", "-i", rawPath, "-vcodec", "libx264", "-acodec", "aac", mp4Path)
	if err := cmd.Run(); err != nil {

		return nil, fmt.Errorf("failed to convert to mp4: %v", err)
	}
	mp4Video, err = os.ReadFile(mp4Path)
	if err != nil {

		return nil, fmt.Errorf("failed to read mp4 file: %v", err)
	}
	defer os.Remove(mp4Path)

	return mp4Video, nil
}

// createDirectoryIfNotExist 检查目录是否存在，不存在则创建
func createDirectoryIfNotExist(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		// 创建目录
		err := os.MkdirAll(path, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}
