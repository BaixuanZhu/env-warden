package broadcast

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 广播参数；常量值与 Winuser.h 定义一致。
const (
	hwndBroadcast   = windows.HWND(0xFFFF) // HWND_BROADCAST：发给所有顶层窗口
	wmSettingChange = 0x001A               // WM_SETTINGCHANGE
	smtoAbortIfHung = 0x0002               // SMTO_ABORTIFHUNG：跳过无响应窗口
)

// user32 缓加载，进程内只解析一次。
var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procSendMessageT = user32.NewProc("SendMessageTimeoutW")
)

// NotifyEnvironmentChange 向所有顶层窗口广播 WM_SETTINGCHANGE（lParam="Environment"），
// 使新开的终端与 Explorer 立即取得新 PATH，无需注销重登。
// 已开着的旧终端持有旧值不受影响，验证必须开新终端。
func NotifyEnvironmentChange() error {
	return NotifyEnvironmentChangeTimeout(5000)
}

// NotifyEnvironmentChangeTimeout 是 NotifyEnvironmentChange 的可调超时版本，
// timeoutMillis 为单个无响应窗口的最长等待毫秒数。
// 广播失败（超时或全部窗口拒绝）返回错误；个别窗口失败由 SMTO_ABORTIFHUNG 跳过，不算错误。
func NotifyEnvironmentChangeTimeout(timeoutMillis uint32) error {
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	var result uintptr
	if _, err := sendMessageTimeout(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, timeoutMillis, &result); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	return nil
}

// sendMessageTimeout 是 SendMessageTimeoutW 的薄封装，参数与 Win32 原生签名一一对应；
// 返回非零错误时调用方不得假定消息已送达。
func sendMessageTimeout(hwnd windows.HWND, msg uint32, wparam, lparam uintptr, flags, timeoutMillis uint32, result *uintptr) (uintptr, error) {
	r1, _, callErr := procSendMessageT.Call(
		uintptr(hwnd), uintptr(msg), wparam, lparam,
		uintptr(flags), uintptr(timeoutMillis), uintptr(unsafe.Pointer(result)),
	)
	if r1 == 0 {
		// Call 的第三个返回值恒为 GetLastError，仅在 r1==0 时有意义。
		return 0, fmt.Errorf("SendMessageTimeoutW: %w", callErr)
	}
	return r1, nil
}
