package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"envwarden/core/registry"
)

// mkSnap 构造两级 PATH 的快照。
func mkSnap(userRaw, userKind, sysRaw, sysKind string, at time.Time) *Snapshot {
	return &Snapshot{
		Version:   currentVersion,
		CreatedAt: at,
		Source:    "test",
		Paths: []PathSnapshot{
			{Scope: "user", Raw: userRaw, Kind: userKind},
			{Scope: "system", Raw: sysRaw, Kind: sysKind},
		},
	}
}

// at 返回固定时刻，保证测试可复现。
func at() time.Time {
	return time.Date(2026, 10, 14, 21, 0, 0, 0, time.Local)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	snap := mkSnap(`C:\a;%MY%\bin`, "REG_SZ", `C:\Windows`, "REG_EXPAND_SZ", at())
	path, err := Save(dir, snap)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Base(path) != "snapshot-20261014-210000.json" {
		t.Errorf("文件名 = %q", filepath.Base(path))
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CreatedAt != snap.CreatedAt || len(got.Paths) != 2 {
		t.Fatalf("回读不一致: %+v", got)
	}
	for i := range got.Paths {
		if got.Paths[i] != snap.Paths[i] {
			t.Errorf("Paths[%d] = %+v, want %+v", i, got.Paths[i], snap.Paths[i])
		}
	}
}

func TestSaveCollisionSuffix(t *testing.T) {
	dir := t.TempDir()
	p1, err := Save(dir, mkSnap("a", "REG_SZ", "b", "REG_SZ", at()))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	p2, err := Save(dir, mkSnap("c", "REG_SZ", "d", "REG_SZ", at()))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("同秒两次保存不应覆盖: %s", p1)
	}
	if !strings.HasSuffix(p2, ".2.json") {
		t.Errorf("冲突文件名 = %q, want 后缀 .2.json", filepath.Base(p2))
	}
	if _, err := Load(p2); err != nil {
		t.Fatalf("Load 冲突文件: %v", err)
	}
}

func TestLatest(t *testing.T) {
	dir := t.TempDir()
	t2 := at().Add(2 * time.Minute)
	t3 := at().Add(3 * time.Minute)
	for _, s := range []*Snapshot{
		mkSnap("newest", "REG_SZ", "s", "REG_SZ", t3),
		mkSnap("oldest", "REG_SZ", "s", "REG_SZ", at()),
		mkSnap("middle", "REG_SZ", "s", "REG_SZ", t2),
	} {
		if _, err := Save(dir, s); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	got, path, err := Latest(dir)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if u, _ := got.Path(registry.ScopeUser); u.Raw != "newest" {
		t.Errorf("Latest 取到 %q, want newest (path=%s)", u.Raw, path)
	}
}

func TestLatestErrors(t *testing.T) {
	if _, _, err := Latest(t.TempDir()); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("空目录: err = %v, want ErrNoSnapshot", err)
	}
	if _, _, err := Latest(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("目录不存在: err = %v, want ErrNoSnapshot", err)
	}
}

func TestLoadBad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bad := []struct {
		name  string
		lines string
	}{
		{"损坏 JSON", "{not json"},
		{"版本过新", `{"version":99,"createdAt":"2026-10-14T21:00:00Z","paths":[{"scope":"user","raw":"a","kind":"REG_SZ"}]}`},
		{"无路径记录", `{"version":1,"createdAt":"2026-10-14T21:00:00Z","paths":[]}`},
	}
	for _, tt := range bad {
		if _, err := Load(write("bad.reg.json", tt.lines)); !errors.Is(err, ErrBadSnapshot) {
			t.Errorf("%s: err = %v, want ErrBadSnapshot", tt.name, err)
		}
	}
}

func TestToPathData(t *testing.T) {
	pd, err := (&PathSnapshot{Scope: "user", Raw: `C:\a;C:\b`, Kind: "REG_SZ"}).ToPathData()
	if err != nil {
		t.Fatalf("ToPathData: %v", err)
	}
	if pd.Scope != registry.ScopeUser || pd.Kind != registry.KindSZ || pd.Raw != `C:\a;C:\b` {
		t.Errorf("ToPathData 结果: %+v", pd)
	}
	for _, bad := range []PathSnapshot{
		{Scope: "both", Raw: "a", Kind: "REG_SZ"},
		{Scope: "user", Raw: "a", Kind: "REG_WEIRD"},
		{Scope: "user", Raw: "a", Kind: "REG_BINARY"},
	} {
		if _, err := bad.ToPathData(); err == nil {
			t.Errorf("%+v 应返回错误", bad)
		}
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name    string
		old     *Snapshot
		new     *Snapshot
		added   []string // 用户级新增条目原文
		removed []string // 用户级删除条目原文
		order   bool     // 用户级顺序变化
		kind    bool     // 用户级类型变化
		count   int      // ChangeCount
	}{
		{
			name:  "无变化",
			old:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			count: 0,
		},
		{
			name:  "新增一条",
			old:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a;C:\new;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			added: []string{`C:\new`},
			count: 1,
		},
		{
			name:    "删除一条",
			old:     mkSnap(`C:\a;C:\gone;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:     mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			removed: []string{`C:\gone`},
			count:   1,
		},
		{
			name:  "新增位置取原始切分下标",
			old:   mkSnap(`C:\a; ;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a; ;C:\b;C:\new`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			added: []string{`C:\new`},
			count: 1,
		},
		{
			name:  "尾部空条目不算变化",
			old:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a;C:\b;`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			count: 0,
		},
		{
			name:  "空白条目出现不算变化",
			old:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a;  ;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			count: 0,
		},
		{
			name:  "交换顺序",
			old:   mkSnap(`C:\a;C:\b;C:\c`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\b;C:\a;C:\c`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			order: true,
			count: 1,
		},
		{
			name:  "增删导致的位移不算顺序变化",
			old:   mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\new;C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			added: []string{`C:\new`},
			count: 1, // 仅新增 1 条，相对顺序未变
		},
		{
			name:  "值类型变化",
			old:   mkSnap(`C:\a`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:   mkSnap(`C:\a`, "REG_EXPAND_SZ", `C:\w`, "REG_SZ", at()),
			kind:  true,
			count: 1,
		},
		{
			name:    "重复条目按次数抵消",
			old:     mkSnap(`C:\a;C:\b;C:\a`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:     mkSnap(`C:\a;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			removed: []string{`C:\a`},
			count:   1,
		},
		{
			name:    "系统级变化独立成段",
			old:     mkSnap(`C:\a`, "REG_SZ", `C:\w;C:\x`, "REG_SZ", at()),
			new:     mkSnap(`C:\a`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			removed: []string{}, // 用户级无变化；系统级删除在另一作用域
			count:   1,
		},
		{
			name:    "中文路径增删",
			old:     mkSnap(`F:\微信web开发者工具\dll;C:\a`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			new:     mkSnap(`C:\a`, "REG_SZ", `C:\w`, "REG_SZ", at()),
			removed: []string{`F:\微信web开发者工具\dll`},
			count:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := Diff(tt.old, tt.new)
			if got := rep.ChangeCount(); got != tt.count {
				t.Errorf("ChangeCount = %d, want %d", got, tt.count)
			}
			var user ScopeDiff
			for _, s := range rep.Scopes {
				if s.Scope == registry.ScopeUser {
					user = s
				}
			}
			if user.KindChanged != tt.kind {
				t.Errorf("KindChanged = %v, want %v", user.KindChanged, tt.kind)
			}
			if user.OrderChanged != tt.order {
				t.Errorf("OrderChanged = %v, want %v (old=%v new=%v)",
					user.OrderChanged, tt.order, user.OldOrder, user.NewOrder)
			}
			var added, removed []string
			for _, a := range user.Added {
				added = append(added, a.Entry)
			}
			for _, r := range user.Removed {
				removed = append(removed, r.Entry)
			}
			if strings.Join(added, "|") != strings.Join(tt.added, "|") {
				t.Errorf("Added = %v, want %v", added, tt.added)
			}
			if strings.Join(removed, "|") != strings.Join(tt.removed, "|") {
				t.Errorf("Removed = %v, want %v", removed, tt.removed)
			}
		})
	}
}

// TestDiffPositions 校验新增/删除条目的位置编号按原始切分 1 起计（与 check 一致）。
func TestDiffPositions(t *testing.T) {
	old := mkSnap(`C:\a; ;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at())
	new := mkSnap(`C:\a; ;C:\new;C:\b`, "REG_SZ", `C:\w`, "REG_SZ", at())
	rep := Diff(old, new)
	if len(rep.Scopes) == 0 || len(rep.Scopes[0].Added) != 1 {
		t.Fatalf("期望用户级 1 条新增: %+v", rep.Scopes)
	}
	// C:\new 前有 1 个空白条目（原始位置 3）。
	if got := rep.Scopes[0].Added[0]; got.Entry != `C:\new` || got.Position != 3 {
		t.Errorf("Added = %+v, want {C:\\new 3}", got)
	}
}
