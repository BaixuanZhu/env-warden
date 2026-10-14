package detect

import (
	"os"
	"strings"

	"envwarden/core/registry"
)

// EntryFinding 描述 PATH 中一个条目的位置、展开结果与存在性。
type EntryFinding struct {
	Scope    registry.Scope // 所属作用域
	Index    int            // 条目在该作用域 PATH 中的位置（0 起，以原始切分计）
	Entry    string         // 原始写法（未展开、未 trim，逐字写回的依据）
	Trimmed  string         // TrimSpace 后的条目（分析基准，与 env-check.ps1 口径一致）
	Expanded string         // %VAR% 展开后的完整目录
	Exists   bool           // 展开后目录是否存在
}

// ScannedEntries 是两级 PATH 条目的一次性扫描结果，是死条目/重复/npm 检测的公共输入。
type ScannedEntries struct {
	System []EntryFinding
	User   []EntryFinding
}

// All 按命令解析顺序（系统级在前、用户级在后）返回全部条目。
func (s *ScannedEntries) All() []EntryFinding {
	out := make([]EntryFinding, 0, len(s.System)+len(s.User))
	out = append(out, s.System...)
	out = append(out, s.User...)
	return out
}

// Total 返回非空白条目总数。
func (s *ScannedEntries) Total() int {
	return len(s.System) + len(s.User)
}

// ScanEntries 展开并检查两级 PATH 的全部条目。
// 空白条目（含尾部 ';' 切分出的空条目）跳过，避免死条目误报。
func ScanEntries(system, user *registry.PathData, vars Vars) *ScannedEntries {
	return &ScannedEntries{
		System: scanScope(system, vars),
		User:   scanScope(user, vars),
	}
}

// dirExists 判断展开后的目录是否存在于磁盘。
// 刻意做成包级变量作为测试接缝：夹具验收用例注入假实现固定"哪些路径存活"，
// 使死条目基线不依赖运行机器的磁盘状态（CI runner 没有 F:/G:/E: 等盘）。
var dirExists = func(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// scanScope 展开单个作用域的条目并逐个做存在性检查。
func scanScope(pd *registry.PathData, vars Vars) []EntryFinding {
	if pd == nil {
		return nil
	}
	out := make([]EntryFinding, 0, len(pd.Entries))
	for i, entry := range pd.Entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		expanded := ExpandEnv(trimmed, vars)
		out = append(out, EntryFinding{
			Scope:    pd.Scope,
			Index:    i,
			Entry:    entry,
			Trimmed:  trimmed,
			Expanded: expanded,
			Exists:   dirExists(expanded),
		})
	}
	return out
}

// DeadEntries 返回死条目（%VAR% 展开后目录不存在）。
func (s *ScannedEntries) DeadEntries() []EntryFinding {
	var out []EntryFinding
	for _, f := range s.All() {
		if !f.Exists {
			out = append(out, f)
		}
	}
	return out
}

// NormalizeEntry 条目归一化：TrimSpace + 去尾部反斜杠 + 小写，用于重复判定。
func NormalizeEntry(entry string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(entry), `\`))
}

// DuplicateGroup 是归一化后互相重复的一组条目，可能跨两级。
type DuplicateGroup struct {
	Normalized string
	Members    []EntryFinding
}

// DuplicateGroups 返回跨两级合并判定的重复分组，按首次出现顺序排列。
func (s *ScannedEntries) DuplicateGroups() []DuplicateGroup {
	var order []string
	groups := make(map[string][]EntryFinding)
	for _, f := range s.All() {
		key := NormalizeEntry(f.Trimmed)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}
	var out []DuplicateGroup
	for _, key := range order {
		if len(groups[key]) > 1 {
			out = append(out, DuplicateGroup{Normalized: key, Members: groups[key]})
		}
	}
	return out
}
