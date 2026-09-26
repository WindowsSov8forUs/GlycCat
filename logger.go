package main

import (
	"context"
	"fmt"

	botgolog "github.com/WindowsSov8forUs/botgo-plus/log"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

// Logger 将两套 SDK 的日志分别接入应用日志器，不相互转发。
type Logger struct {
	source string
}

func (l Logger) Log(_ context.Context, level server.LogLevel, v ...any) {
	if len(v) == 0 {
		v = []any{"收到 Satori 服务事件。"}
	}
	lvl := log.INFO
	switch level {
	case server.LogLevelDebug:
		lvl = log.DEBUG
	case server.LogLevelWarn:
		lvl = log.WARN
	case server.LogLevelError:
		lvl = log.ERROR
	}
	l.print(lvl, v...)
}

// print 只补充来源，级别过滤、脱敏和输出仍由应用日志器负责。
func (l Logger) print(level log.LogLevel, v ...any) {
	message := fmt.Sprint(v...)
	if l.source != "" {
		message = "[" + l.source + "] " + message
	}
	log.GetLogger().Println(level, message)
}

func (l Logger) Debug(v ...interface{}) {
	l.print(log.DEBUG, v...)
}

func (l Logger) Info(v ...interface{}) {
	l.print(log.INFO, v...)
}

func (l Logger) Warn(v ...interface{}) {
	l.print(log.WARN, v...)
}

func (l Logger) Error(v ...interface{}) {
	l.print(log.ERROR, v...)
}

func (l Logger) Debugf(format string, v ...interface{}) {
	l.print(log.DEBUG, fmt.Sprintf(format, v...))
}

func (l Logger) Infof(format string, v ...interface{}) {
	l.print(log.INFO, fmt.Sprintf(format, v...))
}

func (l Logger) Warnf(format string, v ...interface{}) {
	l.print(log.WARN, fmt.Sprintf(format, v...))
}

func (l Logger) Errorf(format string, v ...interface{}) {
	l.print(log.ERROR, fmt.Sprintf(format, v...))
}

func (l Logger) Sync() error {
	return log.GetLogger().Sync()
}

var _ server.Logger = Logger{}
var _ botgolog.Logger = Logger{}
