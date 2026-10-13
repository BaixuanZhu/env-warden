package detect

import (
	"os"
	"path/filepath"
	"testing"

	"envwarden/core/registry"
)

// mustPathData 构造 PathData，失败即中止测试。
func mustPathData(t *testing.T, scope registry.Scope, raw string) *registry.PathData {
	t.Helper()
	pd, err := registry.NewPathData(scope, raw, registry.KindSZ)
	if err != nil {
		t.Fatalf("NewPathData: %v", err)
	}
	return pd
}

func TestNormalizeEntry(t *testing.T) {
	tests := []struct{ in, want string }{
		{`C:\A\`, `c:\a`},
		{` C:\A `, `c:\a`},
		{`C:\A`, `c:\a`},
		{`f:\微信web开发者工具\dll\`, `f:\微信web开发者工具\dll`},
	}
	for _, tt := range tests {
		if got := NormalizeEntry(tt.in); got != tt.want {
			t.Errorf("NormalizeEntry(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScanDeadAndDuplicates(t *testing.T) {
	tmp := t.TempDir() // 存活目录
	raw := tmp + `;X:\no\such\dir;;  ;` + tmp
	scanned := ScanEntries(nil, mustPathData(t, registry.ScopeUser, raw), nil)

	if got := scanned.Total(); got != 3 {
		t.Errorf("非空白条目数: got %d, want 3（空与空白条目跳过）", got)
	}
	dead := scanned.DeadEntries()
	if len(dead) != 1 || dead[0].Trimmed != `X:\no\such\dir` || dead[0].Index != 1 {
		t.Errorf("死条目: got %+v", dead)
	}
	dups := scanned.DuplicateGroups()
	if len(dups) != 1 || len(dups[0].Members) != 2 || dups[0].Normalized != NormalizeEntry(tmp) {
		t.Errorf("重复分组: got %+v", dups)
	}
}

func TestScanCrossScopeDuplicate(t *testing.T) {
	sys := mustPathData(t, registry.ScopeSystem, `C:\Tools\;D:\OnlySys`)
	usr := mustPathData(t, registry.ScopeUser, `c:\tools;E:\OnlyUser`)
	dups := ScanEntries(sys, usr, nil).DuplicateGroups()
	if len(dups) != 1 {
		t.Fatalf("应恰好 1 组跨两级重复, got %d", len(dups))
	}
	if dups[0].Normalized != `c:\tools` {
		t.Errorf("归一化键: got %q", dups[0].Normalized)
	}
	if len(dups[0].Members) != 2 || dups[0].Members[0].Scope != registry.ScopeSystem {
		t.Errorf("成员: got %+v", dups[0].Members)
	}
}

func TestScanExpandsVars(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	vars := Vars{"BASE": tmp}
	scanned := ScanEntries(
		nil,
		mustPathData(t, registry.ScopeUser, `%BASE%\sub;%BASE%\missing`),
		vars,
	)
	if scanned.Total() != 2 {
		t.Fatalf("条目数: got %d", scanned.Total())
	}
	if !scanned.User[0].Exists {
		t.Errorf("%%BASE%%\\sub 展开后应存在: %+v", scanned.User[0])
	}
	if scanned.User[1].Exists {
		t.Errorf("%%BASE%%\\missing 不应存在: %+v", scanned.User[1])
	}
}
