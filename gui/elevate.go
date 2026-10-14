package gui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// RelaunchElevated 以管理员身份重启 GUI（ShellExecute "runas"），
// 用于系统级 PATH 写入前的提权：用户在 UAC 弹窗确认后新实例以管理员运行，
// 当前实例随之退出。用户取消 UAC 时返回错误且不做任何事。
func (a *App) RelaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("relaunch elevated: %w", err)
	}
	if err := windows.ShellExecute(0,
		windows.StringToUTF16Ptr("runas"),
		windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr("gui"),
		windows.StringToUTF16Ptr(filepath.Dir(exe)),
		windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("提权重启被取消或失败: %w", err)
	}
	runtime.Quit(a.ctx)
	return nil
}
