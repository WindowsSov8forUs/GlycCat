package sys

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/WindowsSov8forUs/glyccat/log"
)

// RestartApplication 重启应用
func RestartApplication() {
	executableName, err := GetExecutableName()
	if err != nil {
		log.Fatalf("获取可执行文件名时出错: %v", err)
		os.Exit(1)
	}

	restarter := NewRestarter()
	if err := restarter.Restart(executableName); err != nil {
		log.Fatalf("重启应用时出错: %v", err)
		os.Exit(1)
	}

	if _, err := os.Create("restart.flag"); err != nil {
		log.Fatalf("创建重启标记文件时出错: %v", err)
		os.Exit(1)
	}

	os.Exit(0)
}

// GetExecutableName 获取可执行文件名
func GetExecutableName() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(executable, filepath.Ext(executable)), nil
}

// isRunningInTerminal 检查是否在终端中运行
func isRunningInTerminal() bool {
	// 检查标准输出是否连接到终端
	return os.Stdout.Fd() != 0 && isatty.IsTerminal(os.Stdout.Fd())
}

// InitBase 解析参数并检测
func InitBase() {
	switch runtime.GOOS {
	case "windows":
		if RunningByDoubleClick() {
			err := NoMoreDoubleClick()
			if err != nil {
				log.Errorf("遇到错误: %v", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	case "linux", "darwin":
		if !isRunningInTerminal() {
			log.Warn("未在终端环境中运行，建议在终端中运行以获得更好的体验")
		}
	default:
		log.Infof("当前正在 %s 系统上运行", runtime.GOOS)
	}
}
