package registry

import (
	"errors"
	"testing"
)

func TestSplitPathEntries(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"空字符串返回 nil", "", nil},
		{"单条目", `C:\Go\bin`, []string{`C:\Go\bin`}},
		{"两条目", `C:\a;D:\b`, []string{`C:\a`, `D:\b`}},
		{"中间空条目保留", `C:\a;;D:\b`, []string{`C:\a`, ``, `D:\b`}},
		{"尾部空条目保留", `C:\a;`, []string{`C:\a`, ``}},
		{"首部空条目保留", `;C:\a`, []string{``, `C:\a`}},
		{"中文路径不乱码", `F:\微信web开发者工具\dll;G:\Scoop\shims`, []string{`F:\微信web开发者工具\dll`, `G:\Scoop\shims`}},
		{"%VAR% 不展开", `%SystemRoot%\system32;%USERPROFILE%\bin`, []string{`%SystemRoot%\system32`, `%USERPROFILE%\bin`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitPathEntries(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("条目数不符: got %d (%q), want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("条目[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitJoinRoundTrip(t *testing.T) {
	raws := []string{
		`C:\WINDOWS\system32;C:\WINDOWS;`,
		`C:\a;;D:\b`,
		`;leading;trailing;`,
		`F:\微信web开发者工具\dll`,
		`%SystemRoot%\system32`,
	}
	for _, raw := range raws {
		if got := JoinPathEntries(SplitPathEntries(raw)); got != raw {
			t.Errorf("round trip 失败: got %q, want %q", got, raw)
		}
	}
	if got := JoinPathEntries(SplitPathEntries("")); got != "" {
		t.Errorf("空字符串 round trip 应仍为空, got %q", got)
	}
}

func TestNewPathDataRejectsNonStringKind(t *testing.T) {
	_, err := NewPathData(ScopeUser, `C:\a`, KindBinary)
	if !errors.Is(err, ErrUnexpectedKind) {
		t.Fatalf("期望 ErrUnexpectedKind, got %v", err)
	}
}

func TestNewPathDataAcceptsStringKinds(t *testing.T) {
	for _, kind := range []ValueKind{KindSZ, KindExpandSZ} {
		pd, err := NewPathData(ScopeUser, `C:\a;D:\b`, kind)
		if err != nil {
			t.Fatalf("kind %s 不应报错: %v", kind, err)
		}
		if pd.Kind != kind || len(pd.Entries) != 2 {
			t.Errorf("PathData 构造不符: %+v", pd)
		}
	}
}

func TestScopeKeyPathAndString(t *testing.T) {
	if got := ScopeUser.KeyPath(); got != `HKEY_CURRENT_USER\Environment` {
		t.Errorf("user KeyPath: got %q", got)
	}
	if got := ScopeSystem.KeyPath(); got != `HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Session Manager\Environment` {
		t.Errorf("system KeyPath: got %q", got)
	}
	if ScopeUser.String() != "user" || ScopeSystem.String() != "system" {
		t.Errorf("Scope.String 不符: got %q / %q", ScopeUser, ScopeSystem)
	}
}

func TestValueKindString(t *testing.T) {
	tests := []struct {
		kind ValueKind
		want string
	}{
		{KindSZ, "REG_SZ"},
		{KindExpandSZ, "REG_EXPAND_SZ"},
		{KindDWORD, "REG_DWORD"},
		{ValueKind(42), "REG_42"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ValueKind(%d).String(): got %q, want %q", tt.kind, got, tt.want)
		}
	}
}
