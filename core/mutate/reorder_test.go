package mutate

import (
	"errors"
	"strings"
	"testing"

	"envwarden/core/registry"
)

// mkPD 构造用户级 PathData 测试输入。
func mkPD(t *testing.T, raw string, kind registry.ValueKind) *registry.PathData {
	t.Helper()
	pd, err := registry.NewPathData(registry.ScopeUser, raw, kind)
	if err != nil {
		t.Fatalf("NewPathData: %v", err)
	}
	return pd
}

func TestReorderVisible(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		ordered []string
		wantRaw string
		wantErr error
	}{
		{
			name:    "两条目交换",
			raw:     `C:\a;C:\b;C:\c`,
			ordered: []string{`C:\c`, `C:\a`, `C:\b`},
			wantRaw: `C:\c;C:\a;C:\b`,
		},
		{
			name:    "顺序不变为合法空操作",
			raw:     `C:\a;C:\b`,
			ordered: []string{`C:\a`, `C:\b`},
			wantRaw: `C:\a;C:\b`,
		},
		{
			name:    "空白条目保持原位",
			raw:     `C:\a;;C:\b;`,
			ordered: []string{`C:\b`, `C:\a`},
			wantRaw: `C:\b;;C:\a;`,
		},
		{
			name:    "仅空白差异的条目视为不同条目",
			raw:     `C:\a; C:\b`,
			ordered: []string{`C:\b`, `C:\a `},
			wantErr: ErrNotPermutation,
		},
		{
			name:    "缺少条目",
			raw:     `C:\a;C:\b;C:\c`,
			ordered: []string{`C:\b`, `C:\a`},
			wantErr: ErrNotPermutation,
		},
		{
			name:    "混入新条目",
			raw:     `C:\a;C:\b`,
			ordered: []string{`C:\a`, `C:\new`},
			wantErr: ErrNotPermutation,
		},
		{
			name:    "混入空白条目",
			raw:     `C:\a;C:\b`,
			ordered: []string{`C:\a`, `C:\b`, ``},
			wantErr: ErrNotPermutation,
		},
		{
			name:    "重复条目次数必须一致",
			raw:     `C:\a;C:\a;C:\b`,
			ordered: []string{`C:\a`, `C:\b`, `C:\b`},
			wantErr: ErrNotPermutation,
		},
		{
			name:    "重复条目成对保留",
			raw:     `C:\a;C:\a;C:\b`,
			ordered: []string{`C:\b`, `C:\a`, `C:\a`},
			wantRaw: `C:\b;C:\a;C:\a`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := mkPD(t, tt.raw, registry.KindExpandSZ)
			plan, err := ReorderVisible(pd, tt.ordered)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReorderVisible err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReorderVisible: %v", err)
			}
			if plan.NewRaw != tt.wantRaw {
				t.Errorf("NewRaw = %q, want %q", plan.NewRaw, tt.wantRaw)
			}
			if plan.Kind != registry.KindExpandSZ {
				t.Errorf("Kind = %s, want 保持 REG_EXPAND_SZ", plan.Kind)
			}
			// NewRaw 必须能无损切回 Ordered。
			if got := registry.SplitPathEntries(plan.NewRaw); strings.Join(got, ";") != plan.NewRaw {
				t.Errorf("NewRaw 切分往返不一致: %q", got)
			}
		})
	}
}

func TestMoveVisible(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		from    int
		to      int
		wantRaw string
	}{
		{name: "首条移到末尾", raw: `C:\a;C:\b;C:\c`, from: 1, to: 3, wantRaw: `C:\b;C:\c;C:\a`},
		{name: "末条移到首位", raw: `C:\a;C:\b;C:\c`, from: 3, to: 1, wantRaw: `C:\c;C:\a;C:\b`},
		{name: "原位移动", raw: `C:\a;C:\b`, from: 2, to: 2, wantRaw: `C:\a;C:\b`},
		{name: "空白条目不占序号", raw: `C:\a;;C:\b`, from: 2, to: 1, wantRaw: `C:\b;;C:\a`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := mkPD(t, tt.raw, registry.KindSZ)
			plan, err := MoveVisible(pd, tt.from, tt.to)
			if err != nil {
				t.Fatalf("MoveVisible: %v", err)
			}
			if plan.NewRaw != tt.wantRaw {
				t.Errorf("NewRaw = %q, want %q", plan.NewRaw, tt.wantRaw)
			}
		})
	}

	t.Run("序号越界", func(t *testing.T) {
		pd := mkPD(t, `C:\a;C:\b`, registry.KindSZ)
		if _, err := MoveVisible(pd, 0, 1); err == nil {
			t.Error("from=0 应报错")
		}
		if _, err := MoveVisible(pd, 1, 3); err == nil {
			t.Error("to 越界应报错")
		}
	})
}

func TestVisibleEntries(t *testing.T) {
	pd := mkPD(t, ` C:\a ;; ;C:\b;`, registry.KindSZ)
	got := VisibleEntries(pd)
	// 空白条目被剔除，但条目原文（含首尾空格）逐字保留。
	want := []string{` C:\a `, `C:\b`}
	if len(got) != len(want) {
		t.Fatalf("VisibleEntries = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("VisibleEntries[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
