package sys

import (
	"os"
	"runtime"

	"github.com/mattn/go-isatty"

	"github.com/WindowsSov8forUs/glyccat/log"
)

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
