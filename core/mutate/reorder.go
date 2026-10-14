package mutate

import (
	"errors"
	"fmt"
	"strings"

	"envwarden/core/registry"
)

// ErrNotPermutation 表示提交的新顺序与当前 PATH 条目不构成逐字节全等的排列。
// PATH 可能在展示后被外部修改，调用方收到该错误必须放弃写入并重新体检。
var ErrNotPermutation = errors.New("reordered entries are not a permutation of current PATH")

// ReorderPlan 是一次顺序调整的完整计划，是写回注册表前的唯一依据。
// 条目原文逐字节不变、仅顺序变化；PATH 顺序即命令解析优先级，是命令抢占问题的根源。
type ReorderPlan struct {
	// Scope 目标作用域。
	Scope registry.Scope
	// NewRaw 重排后的新 PATH 原始串（含保持原位的空白条目）。
	NewRaw string
	// Kind 原值类型（REG_SZ / REG_EXPAND_SZ），写回时必须原样保持。
	Kind registry.ValueKind
	// Ordered 重排后的完整条目列表（含空白条目）。
	Ordered []string
}

// VisibleEntries 返回 PATH 中的非空白条目，即 check 报告与 GUI 列表展示的口径。
func VisibleEntries(pd *registry.PathData) []string {
	out := make([]string, 0, len(pd.Entries))
	for _, e := range pd.Entries {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}

// ReorderVisible 按 ordered 给出的新顺序重排 pd 的非空白条目并生成写回计划。
//
// ordered 必须与当前非空白条目构成逐字节全等的排列——只允许调整顺序，
// 不允许增删或任何改写（含空白字符差异）；空白条目不参与重排，保持原位。
// 校验失败返回包装 ErrNotPermutation 的错误，调用方应放弃写入并重新体检。
func ReorderVisible(pd *registry.PathData, ordered []string) (*ReorderPlan, error) {
	visible := VisibleEntries(pd)
	if !sameMultiset(visible, ordered) {
		return nil, fmt.Errorf("reorder %s: %w（新顺序必须与当前条目逐字节一致，仅允许调整顺序）", pd.Scope, ErrNotPermutation)
	}
	merged := make([]string, 0, len(pd.Entries))
	next := 0
	for _, e := range pd.Entries {
		if strings.TrimSpace(e) == "" {
			merged = append(merged, e)
			continue
		}
		merged = append(merged, ordered[next])
		next++
	}
	return &ReorderPlan{
		Scope:   pd.Scope,
		NewRaw:  registry.JoinPathEntries(merged),
		Kind:    pd.Kind,
		Ordered: merged,
	}, nil
}

// MoveVisible 把第 from 个非空白条目移动到第 to 个位置（均 1 起，按移动后的列表计），
// 生成重排计划。序号口径与 check 报告 / GUI 列表的可见条目一致，空白条目不占序号。
func MoveVisible(pd *registry.PathData, from, to int) (*ReorderPlan, error) {
	visible := VisibleEntries(pd)
	if from < 1 || from > len(visible) || to < 1 || to > len(visible) {
		return nil, fmt.Errorf("move %s: position out of range: from=%d to=%d (visible entries: %d)",
			pd.Scope, from, to, len(visible))
	}
	ordered := append([]string(nil), visible...)
	entry := ordered[from-1]
	ordered = append(ordered[:from-1], ordered[from:]...)
	idx := to - 1
	if idx > len(ordered) {
		idx = len(ordered)
	}
	ordered = append(ordered, "")
	copy(ordered[idx+1:], ordered[idx:])
	ordered[idx] = entry
	return ReorderVisible(pd, ordered)
}

// sameMultiset 判断两个列表是否构成逐字节全等的排列（含重复条目次数一致）。
func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
