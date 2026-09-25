package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WindowsSov8forUs/glyccat/log"
)

const stderrLimit = 32 * 1024

type stderrBuffer struct{ data []byte }

func (b *stderrBuffer) Write(p []byte) (int, error) {
	size := len(p)
	if size >= stderrLimit {
		b.data = append(b.data[:0], p[size-stderrLimit:]...)
		return size, nil
	}
	if drop := len(b.data) + size - stderrLimit; drop > 0 {
		copy(b.data, b.data[drop:])
		b.data = b.data[:len(b.data)-drop]
	}
	b.data = append(b.data, p...)
	return size, nil
}

// Error 保留转换阶段、退出状态和有界诊断，取消原因仍可通过 errors.Is 判断。
type Error struct {
	Stage    string
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	text := e.Stage + "失败: " + log.SafeText(fmt.Sprint(e.Cause))
	if e.Stderr != "" {
		detail := []rune(e.Stderr)
		if len(detail) > 2048 {
			detail = append(detail[len(detail)-2048:], []rune("（仅显示末尾诊断）")...)
		}
		text += "；" + string(detail)
	}
	return text
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Run 只收集有界 stderr，不改变命令参数、输出文件或超时设置。
func Run(ctx context.Context, cmd *exec.Cmd, stage string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var stderr stderrBuffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil && ctx.Err() == nil {
		return nil
	}
	cause := errors.Join(err, ctx.Err())
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	detail := string(stderr.data)
	if cmd.Dir != "" {
		detail = strings.ReplaceAll(detail, cmd.Dir, "[临时目录]")
		detail = strings.ReplaceAll(detail, strings.ReplaceAll(cmd.Dir, "\\", "/"), "[临时目录]")
	}
	return &Error{Stage: stage, ExitCode: code, Stderr: log.SafeText(detail), Cause: cause}
}
