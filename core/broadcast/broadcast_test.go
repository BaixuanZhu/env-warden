package broadcast

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestSendMessageTimeoutInvalidWindow 用非法窗口句柄验证错误路径：
// 调用失败必须返回错误，而不是静默认为广播成功。
func TestSendMessageTimeoutInvalidWindow(t *testing.T) {
	var result uintptr
	if _, err := sendMessageTimeout(windows.HWND(0x1), wmSettingChange, 0, 0, smtoAbortIfHung, 100, &result); err == nil {
		t.Fatal("对非法窗口句柄调用应返回错误，实际为 nil")
	}
}
