package detect

import (
	"os"
	"path/filepath"
	"strings"

	"envwarden/core/registry"
)

// defaultPathExt Windows 缺省可执行扩展名（PATHEXT 缺失时的回退值）。
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// PathExtFromVars 从变量表取 PATHEXT 并拆分为扩展名列表（统一带前导 '.'）；
// 缺失时回退 Windows 缺省值。
func PathExtFromVars(vars Vars) []string {
	raw, ok := vars["PATHEXT"]
	if !ok || strings.TrimSpace(raw) == "" {
		raw = defaultPathExt
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		out = append(out, p)
	}
	return out
}

// CommandResolution 是一个命令在"新终端视角"下的解析结果。
type CommandResolution struct {
	Command   string
	Found     bool
	First     string         // 第一命中完整路径（与 where.exe 首行一致）
	All       []string       // 全部命中，按解析顺序（与 where.exe 全输出一致）
	FromScope registry.Scope // 第一命中所在作用域
	FromIndex int            // 第一命中条目在该作用域 PATH 中的位置（0 起）
	FromEntry string         // 第一命中的 PATH 条目（原始写法）
}

// ResolveCommands 按新终端视角解析命令位置。
//
// 解析顺序与 Windows 一致：系统级 PATH 在前、用户级在后（同名命令前者胜出）；
// 每个目录内先试原名、再按 PATHEXT 顺序试扩展名，每个目录只取第一个命中。
func ResolveCommands(system, user *registry.PathData, vars Vars, pathext []string, commands []string) []CommandResolution {
	out := make([]CommandResolution, 0, len(commands))
	for _, cmd := range commands {
		out = append(out, resolveOne(system, user, vars, pathext, cmd))
	}
	return out
}

func resolveOne(system, user *registry.PathData, vars Vars, pathext []string, cmd string) CommandResolution {
	res := CommandResolution{Command: cmd}
	for _, pd := range []*registry.PathData{system, user} {
		if pd == nil {
			continue
		}
		for i, entry := range pd.Entries {
			dir := ExpandEnv(strings.TrimSpace(entry), vars)
			if dir == "" {
				continue
			}
			full := firstExistingIn(dir, cmd, pathext)
			if full == "" {
				continue
			}
			res.All = append(res.All, full)
			if !res.Found {
				res.Found = true
				res.First = full
				res.FromScope = pd.Scope
				res.FromIndex = i
				res.FromEntry = entry
			}
		}
	}
	return res
}

// firstExistingIn 返回 dir 下命令的第一个候选文件（原名优先，其次 PATHEXT 顺序）。
// firstExistingIn 返回 dir 下命令的第一个候选文件（原名优先，其次 PATHEXT 顺序），
// 文件名取磁盘真实写法（与 where.exe 输出一致；NTFS 大小写不敏感，直接拼路径会丢失真实写法）。
func firstExistingIn(dir, cmd string, pathext []string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	lower := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			lower[strings.ToLower(e.Name())] = e.Name()
		}
	}
	for _, cand := range commandCandidates(cmd, pathext) {
		if real, ok := lower[strings.ToLower(cand)]; ok {
			return filepath.Join(dir, real)
		}
	}
	return ""
}

// commandCandidates 生成候选文件名：原名在前，其后依次追加 PATHEXT 扩展名。
func commandCandidates(cmd string, pathext []string) []string {
	out := make([]string, 0, len(pathext)+1)
	out = append(out, cmd)
	for _, ext := range pathext {
		out = append(out, cmd+ext)
	}
	return out
}
