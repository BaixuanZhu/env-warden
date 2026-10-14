package mutate

import (
	"errors"
	"strings"
	"testing"

	"envwarden/core/registry"
)

// newPathData 构造测试用 PathData；错误不可能发生（类型固定合法）。
func newPathData(t *testing.T, scope registry.Scope, raw string, kind registry.ValueKind) *registry.PathData {
	t.Helper()
	pd, err := registry.NewPathData(scope, raw, kind)
	if err != nil {
		t.Fatalf("NewPathData: %v", err)
	}
	return pd
}

func TestRemoveExact(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		kind    registry.ValueKind
		targets []string
		newRaw  string
		removed []string
		kept    []string
	}{
		{
			name:    "单目标移除",
			raw:     `C:\a;C:\b;C:\c`,
			kind:    registry.KindSZ,
			targets: []string{`C:\b`},
			newRaw:  `C:\a;C:\c`,
			removed: []string{`C:\b`},
			kept:    []string{`C:\a`, `C:\c`},
		},
		{
			name:    "多目标且其余条目顺序不变",
			raw:     `C:\a;C:\b;C:\c;C:\d`,
			kind:    registry.KindSZ,
			targets: []string{`C:\d`, `C:\b`},
			newRaw:  `C:\a;C:\c`,
			removed: []string{`C:\b`, `C:\d`},
			kept:    []string{`C:\a`, `C:\c`},
		},
		{
			name:    "同一目标重复出现全部移除",
			raw:     `C:\a;C:\b;C:\a`,
			kind:    registry.KindSZ,
			targets: []string{`C:\a`},
			newRaw:  `C:\b`,
			removed: []string{`C:\a`, `C:\a`},
			kept:    []string{`C:\b`},
		},
		{
			name:    "尾部空条目逐字保留",
			raw:     `C:\a;C:\b;`,
			kind:    registry.KindSZ,
			targets: []string{`C:\b`},
			newRaw:  `C:\a;`,
			removed: []string{`C:\b`},
			kept:    []string{`C:\a`, ``},
		},
		{
			name:    "中文路径目标",
			raw:     `F:\微信web开发者工具\dll;E:\nvm\.nodejs`,
			kind:    registry.KindExpandSZ,
			targets: []string{`F:\微信web开发者工具\dll`},
			newRaw:  `E:\nvm\.nodejs`,
			removed: []string{`F:\微信web开发者工具\dll`},
			kept:    []string{`E:\nvm\.nodejs`},
		},
		{
			name:    "目标带首尾空格按原文命中",
			raw:     `C:\a; C:\b `,
			kind:    registry.KindSZ,
			targets: []string{` C:\b `},
			newRaw:  `C:\a`,
			removed: []string{` C:\b `},
			kept:    []string{`C:\a`},
		},
		{
			name:    "全部条目移除后为空串",
			raw:     `C:\a;C:\b`,
			kind:    registry.KindSZ,
			targets: []string{`C:\a`, `C:\b`},
			newRaw:  ``,
			removed: []string{`C:\a`, `C:\b`},
			kept:    []string{},
		},
		{
			name:    "目标重复声明视为一个",
			raw:     `C:\a;C:\b`,
			kind:    registry.KindSZ,
			targets: []string{`C:\a`, `C:\a`},
			newRaw:  `C:\b`,
			removed: []string{`C:\a`},
			kept:    []string{`C:\b`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := newPathData(t, registry.ScopeUser, tt.raw, tt.kind)
			plan, err := RemoveExact(pd, tt.targets)
			if err != nil {
				t.Fatalf("RemoveExact: %v", err)
			}
			if plan.NewRaw != tt.newRaw {
				t.Errorf("NewRaw = %q, want %q", plan.NewRaw, tt.newRaw)
			}
			if strings.Join(plan.Removed, "|") != strings.Join(tt.removed, "|") {
				t.Errorf("Removed = %v, want %v", plan.Removed, tt.removed)
			}
			if strings.Join(plan.Kept, "|") != strings.Join(tt.kept, "|") {
				t.Errorf("Kept = %v, want %v", plan.Kept, tt.kept)
			}
			if plan.Kind != tt.kind {
				t.Errorf("Kind = %v, want %v", plan.Kind, tt.kind)
			}
			// 逐字保留红线：剩余条目重组后必须与 NewRaw 一致。
			if registry.JoinPathEntries(plan.Kept) != plan.NewRaw {
				t.Errorf("Kept 重组 != NewRaw: %q vs %q", registry.JoinPathEntries(plan.Kept), plan.NewRaw)
			}
		})
	}
}

func TestRemoveExactErrors(t *testing.T) {
	pd := newPathData(t, registry.ScopeUser, `C:\a;C:\b`, registry.KindSZ)

	if _, err := RemoveExact(pd, nil); !errors.Is(err, ErrNoTargets) {
		t.Errorf("空目标: err = %v, want ErrNoTargets", err)
	}

	// 大小写敏感：目标大小写不同即未命中。
	_, err := RemoveExact(pd, []string{`c:\a`})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("大小写不同: err = %v, want ErrTargetNotFound", err)
	}
	if !strings.Contains(err.Error(), `c:\a`) {
		t.Errorf("错误信息应包含未命中目标原文: %v", err)
	}

	// 不 trim：带空格的条目不能用去空格版本命中。
	if _, err := RemoveExact(pd, []string{`C:\a `}); !errors.Is(err, ErrTargetNotFound) {
		t.Errorf("不 trim 命中: err = %v, want ErrTargetNotFound", err)
	}

	// 部分命中同样整体失败，调用方应放弃写入。
	if _, err := RemoveExact(pd, []string{`C:\a`, `C:\missing`}); !errors.Is(err, ErrTargetNotFound) {
		t.Errorf("部分命中: err = %v, want ErrTargetNotFound", err)
	}
}

func TestLocateExact(t *testing.T) {
	raw := `C:\a;C:\b;C:\a`
	found, missing := LocateExact(raw, []string{`C:\a`, `C:\x`, `C:\b`})
	if strings.Join(found, "|") != `C:\a|C:\b` {
		t.Errorf("found = %v, want [C:\\a C:\\b]", found)
	}
	if strings.Join(missing, "|") != `C:\x` {
		t.Errorf("missing = %v, want [C:\\x]", missing)
	}
	if _, missing := LocateExact(raw, nil); len(missing) != 0 {
		t.Errorf("空目标不应产生 missing: %v", missing)
	}
}
