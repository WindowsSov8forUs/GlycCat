//go:build windows
// +build windows

package sys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunningByDoubleClick 是否通过双击运行
func RunningByDoubleClick() bool {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleProcessList")
	if err := proc.Find(); err != nil {
		return false
	}
	var pids [2]uint32
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids)), uintptr(len(pids)))
	// 没有控制台或调用失败时不能推断为双击，后台运行也不能弹出窗口。
	return ret == 1
}

// NoMoreDoubleClick 提示不要双击运行，并生成启动脚本
func NoMoreDoubleClick() error {
	toHighDPI()
	r := boxW(getConsoleWindows(), "请勿通过双击直接运行本程序, 这将导致一些非预料的后果.\n请在shell中运行 GlycCat \n点击确认将释出安全启动脚本，点击取消则关闭程序", "警告", 0x00000030|0x00000001)
	if r == 2 {
		return nil
	}
	r = boxW(0, "点击确认将覆盖 run.bat，点击取消则关闭程序", "警告", 0x00000030|0x00000001)
	if r == 2 {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(filepath.Dir(executable), "run.bat"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	// 从程序所在目录启动，保留空格、中文路径以及传入参数。
	name := strings.ReplaceAll(filepath.Base(executable), "%", "%%")
	_, writeErr := file.WriteString("@echo off\r\ncd /d \"%~dp0\"\r\n\"%~dp0" + name + "\" %*\r\npause\r\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	boxW(0, "已释出安全启动脚本，请双击 run.bat 启动", "提示", 0x00000000)
	return nil
}

// toHighDPI tries to raise DPI awareness context to DPI_AWARENESS_CONTEXT_UNAWARE_GDISCALED
func toHighDPI() {
	systemAware := ^uintptr(2) + 1
	unawareGDIScaled := ^uintptr(5) + 1
	u32 := windows.NewLazySystemDLL("user32.dll")
	proc := u32.NewProc("SetThreadDpiAwarenessContext")
	if proc.Find() != nil {
		return
	}
	for i := unawareGDIScaled; i <= systemAware; i++ {
		_, _, _ = u32.NewProc("SetThreadDpiAwarenessContext").Call(i)
	}
}

// BoxW of Win32 API. Check https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-messageboxw for more detail.
func boxW(hwnd uintptr, caption, title string, flags uint) int {
	captionPtr, _ := windows.UTF16PtrFromString(caption)
	titlePtr, _ := windows.UTF16PtrFromString(title)
	u32 := windows.NewLazySystemDLL("user32.dll")
	ret, _, _ := u32.NewProc("MessageBoxW").Call(
		hwnd,
		uintptr(unsafe.Pointer(captionPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(flags))

	return int(ret)
}

// GetConsoleWindows retrieves the window handle used by the console associated with the calling process.
func getConsoleWindows() (hWnd uintptr) {
	hWnd, _, _ = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	return
}
