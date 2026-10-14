package snapshot

import (
	"strings"
	"time"

	"envwarden/core/registry"
)

// EntryChange 描述一个新增或删除的条目及其所在位置。
type EntryChange struct {
	// Entry 条目原文（逐字节）。
	Entry string
	// Position 条目在该侧 PATH 原始切分中的 1 起位置，与 check 报告口径一致。
	Position int
}

// ScopeDiff 是单个作用域两级快照间的 PATH 差异。
type ScopeDiff struct {
	// Scope 作用域。
	Scope registry.Scope
	// KindChanged 表示 Path 值类型发生变化（REG_SZ ↔ REG_EXPAND_SZ，影响 %VAR% 展开）。
	KindChanged bool
	// OldKind / NewKind 新旧值类型。
	OldKind, NewKind registry.ValueKind
	// Added 新增条目（新快照中有、旧快照中没有）。
	Added []EntryChange
	// Removed 删除条目（旧快照中有、新快照中没有）。
	Removed []EntryChange
	// OrderChanged 表示存活条目的相对顺序变化（顺序即命令解析优先级）。
	OrderChanged bool
	// OldOrder / NewOrder 存活条目在两侧的原始顺序（OrderChanged 时用于展示）。
	OldOrder, NewOrder []string
}

// DiffReport 是一次快照对比的完整结果，固定包含两个作用域的差异。
type DiffReport struct {
	// OldAt / NewAt 两侧快照的创建时间。
	OldAt, NewAt time.Time
	// OldSource / NewSource 两侧快照来源（check / diff-current 等），用于报告展示。
	OldSource, NewSource string
	// Scopes 各作用域差异（用户级在前）。
	Scopes []ScopeDiff
}

// ChangeCount 返回全部作用域的变化计数（新增 + 删除 + 类型变化 + 顺序变化）。
func (r *DiffReport) ChangeCount() int {
	n := 0
	for _, s := range r.Scopes {
		n += len(s.Added) + len(s.Removed)
		if s.KindChanged {
			n++
		}
		if s.OrderChanged {
			n++
		}
	}
	return n
}

// Diff 计算两个快照间的 PATH 差异：按条目原文逐字节比较，空白条目忽略（尾部 ';' 噪音），
// 顺序只看存活条目的相对次序；作用域记录缺失时该作用域不计入结果。
func Diff(old, cur *Snapshot) *DiffReport {
	rep := &DiffReport{
		OldAt:     old.CreatedAt,
		NewAt:     cur.CreatedAt,
		OldSource: old.Source,
		NewSource: cur.Source,
	}
	for _, scope := range []registry.Scope{registry.ScopeUser, registry.ScopeSystem} {
		oldP, err := old.Path(scope)
		if err != nil {
			continue
		}
		newP, err := cur.Path(scope)
		if err != nil {
			continue
		}
		rep.Scopes = append(rep.Scopes, diffScope(oldP, newP))
	}
	return rep
}

// diffScope 计算单作用域差异。
func diffScope(oldP, newP PathSnapshot) ScopeDiff {
	d := ScopeDiff{
		Scope:       mustScope(oldP.Scope),
		OldKind:     mustKind(oldP.Kind),
		NewKind:     mustKind(newP.Kind),
		KindChanged: oldP.Kind != newP.Kind,
	}

	// 位置用原始切分的 1 起下标（与 check 报告一致）；空白条目不参与对比但占位。
	oldEntries, oldPos := indexedEntries(oldP.Raw)
	newEntries, newPos := indexedEntries(newP.Raw)

	// 多重集差异：同一原文出现多次时按次数抵消，剩余部分计入增删。
	oldCount := countEntries(oldEntries)
	newCount := countEntries(newEntries)
	for j, e := range oldEntries {
		if newCount[e] > 0 {
			newCount[e]--
			continue
		}
		d.Removed = append(d.Removed, EntryChange{Entry: e, Position: oldPos[j]})
	}
	for j, e := range newEntries {
		if oldCount[e] > 0 {
			oldCount[e]--
			continue
		}
		d.Added = append(d.Added, EntryChange{Entry: e, Position: newPos[j]})
	}

	// 相对顺序：存活 = 该条目（按出现次数）在对侧仍存在；两侧各自按原顺序过滤后比较。
	newSurviving := countEntries(newEntries)
	for _, e := range oldEntries {
		if newSurviving[e] > 0 {
			d.OldOrder = append(d.OldOrder, e)
			newSurviving[e]--
		}
	}
	oldSurviving := countEntries(oldEntries)
	for _, e := range newEntries {
		if oldSurviving[e] > 0 {
			d.NewOrder = append(d.NewOrder, e)
			oldSurviving[e]--
		}
	}
	d.OrderChanged = !equalStrings(d.OldOrder, d.NewOrder)
	return d
}

// indexedEntries 切分 PATH，剔除空白条目，返回非空条目与其在原始切分中的 1 起位置。
func indexedEntries(raw string) (entries []string, positions []int) {
	for i, e := range registry.SplitPathEntries(raw) {
		if strings.TrimSpace(e) == "" {
			continue
		}
		entries = append(entries, e)
		positions = append(positions, i+1)
	}
	return entries, positions
}

// countEntries 统计各条目出现次数。
func countEntries(entries []string) map[string]int {
	m := make(map[string]int, len(entries))
	for _, e := range entries {
		m[e]++
	}
	return m
}

// equalStrings 比较两个字符串切片是否完全相等。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mustScope 由作用域名取值；未知名返回 user（仅用于展示，解析错误已在加载时拦截）。
func mustScope(s string) registry.Scope {
	sc, _ := registry.ParseScope(s)
	return sc
}

// mustKind 由类型名取值；未知名返回 0（KindChanged 已由原文名比较捕获差异）。
func mustKind(s string) registry.ValueKind {
	k, _ := registry.ParseValueKind(s)
	return k
}
