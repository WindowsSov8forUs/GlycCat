package log

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Start 在配置完成后开启日志文件，不在导入包时写入工作目录
func Start() error {
	logger.Mutex.Lock()
	defer logger.Mutex.Unlock()
	if logger.Level == OFF || logger.lumberjack != nil {
		return nil
	}
	if err := os.MkdirAll("log", 0700); err != nil {
		return fmt.Errorf("创建日志目录时出错: %w", err)
	}
	logger.lumberjack = &lumberjack.Logger{
		Filename:   "log/glyc-cat.log",
		MaxSize:    256,
		MaxAge:     7,
		MaxBackups: 10,
		LocalTime:  true,
	}
	logger.SetOutput(io.MultiWriter(os.Stdout, plainWriter{logger.lumberjack}))
	return nil
}

// Close 关闭日志文件，防止退出后残留文件句柄
func Close() error {
	logger.Mutex.Lock()
	defer logger.Mutex.Unlock()
	logger.SetOutput(io.Discard)
	if logger.lumberjack == nil {
		return nil
	}
	err := logger.lumberjack.Close()
	logger.lumberjack = nil
	return err
}

var colorEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type plainWriter struct {
	writer io.Writer
}

// Write 文件输出去除终端颜色，不改变控制台显示
func (w plainWriter) Write(data []byte) (int, error) {
	plain := colorEscape.ReplaceAll(data, nil)
	n, err := w.writer.Write(plain)
	if err != nil {
		return 0, err
	}
	if n != len(plain) {
		return 0, io.ErrShortWrite
	}
	return len(data), nil
}
