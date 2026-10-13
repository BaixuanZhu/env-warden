package registry

import (
	"strings"
	"testing"
)

// TestReadPathLive 在本机真实注册表上做只读验证（HKLM 读取不需要管理员权限）。
func TestReadPathLive(t *testing.T) {
	for _, scope := range []Scope{ScopeUser, ScopeSystem} {
		pd, err := ReadPath(scope)
		if err != nil {
			t.Errorf("读取 %s PATH 失败: %v", scope, err)
			continue
		}
		if pd.Kind != KindSZ && pd.Kind != KindExpandSZ {
			t.Errorf("%s PATH 类型异常: %s", scope, pd.Kind)
		}
		if pd.Raw == "" || len(pd.Entries) == 0 {
			t.Errorf("%s PATH 为空: %+v", scope, pd)
		}
		// REG_SZ 存储数据带 NUL 终止符，解码后不应残留。
		if strings.ContainsRune(pd.Raw, 0) {
			t.Errorf("%s PATH 含 NUL（终止符未去除）", scope)
		}
	}
}
