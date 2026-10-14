package mutate

import (
	"errors"
	"fmt"
	"strings"

	"envwarden/core/registry"
)

// ErrTargetNotFound 表示声明要移除的条目在当前 PATH 原文中不存在。
// PATH 可能在检测后被外部修改，调用方收到该错误必须放弃写入。
var ErrTargetNotFound = errors.New("target entry not found in PATH")

// ErrNoTargets 表示调用方未声明任何要移除的条目。
var ErrNoTargets = errors.New("no target entries declared")

// RemovalPlan 是一次精确移除的完整计划，是写回注册表前的唯一依据。
type RemovalPlan struct {
	// Scope 目标作用域。
	Scope registry.Scope
	// NewRaw 移除后的新 PATH 原始串（其余条目逐字保留）。
	NewRaw string
	// Kind 原值类型（REG_SZ / REG_EXPAND_SZ），写回时必须原样保持。
	Kind registry.ValueKind
	// Removed 实际移除的条目原文，同一目标多次出现则多次记录。
	Removed []string
	// Kept 逐字保留的条目（含空条目，保持原始切分信息）。
	Kept []string
}

// RemoveExact 在 pd 的 PATH 原文中精确移除 targets 声明的条目。
//
// 匹配规则：按条目原文逐字节比较，不做 trim、不忽略大小写——
// targets 必须与检测报告输出的条目原文完全一致（可从 check 报告直接复制）。
// 同一目标多次出现时全部移除；空条目永不作为目标；
// 任一目标完全不存在时返回包装 ErrTargetNotFound 的错误（列出未命中项），调用方应放弃写入。
func RemoveExact(pd *registry.PathData, targets []string) (*RemovalPlan, error) {
	deduped := dedupe(targets)
	if len(deduped) == 0 {
		return nil, ErrNoTargets
	}

	remaining := make(map[string]int, len(deduped))
	for _, t := range deduped {
		remaining[t] = 0
	}
	kept := make([]string, 0, len(pd.Entries))
	var removed []string
	for _, entry := range pd.Entries {
		if _, isTarget := remaining[entry]; isTarget {
			remaining[entry]++
			removed = append(removed, entry)
			continue
		}
		kept = append(kept, entry)
	}

	var missing []string
	for _, t := range deduped {
		if remaining[t] == 0 {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("remove exact %s: %w: %s",
			pd.Scope, ErrTargetNotFound, strings.Join(quoteAll(missing), ", "))
	}
	return &RemovalPlan{
		Scope:   pd.Scope,
		NewRaw:  registry.JoinPathEntries(kept),
		Kind:    pd.Kind,
		Removed: removed,
		Kept:    kept,
	}, nil
}

// LocateExact 在 raw 的条目中定位 targets：返回至少逐字节命中一次的目标与完全未命中的目标。
// 供调用方先确定某个手写条目属于哪个作用域，再做移除；两个列表均按 targets 声明顺序。
func LocateExact(raw string, targets []string) (found, missing []string) {
	set := make(map[string]bool)
	for _, e := range registry.SplitPathEntries(raw) {
		set[e] = true
	}
	for _, t := range dedupe(targets) {
		if set[t] {
			found = append(found, t)
		} else {
			missing = append(missing, t)
		}
	}
	return found, missing
}

// dedupe 去重并保持首次出现顺序；空目标项剔除。
func dedupe(targets []string) []string {
	seen := make(map[string]bool, len(targets))
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// quoteAll 给每个条目加双引号（不做转义），避免含空格路径在错误信息中产生歧义，
// 且保证用户能按原文复制回 -entry 参数。
func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = `"` + s + `"`
	}
	return out
}
