package detect

import "envwarden/core/registry"

// CheckReport 是一次完整体检的结果汇总，check 子命令与 GUI 共用。
type CheckReport struct {
	System *registry.PathData
	User   *registry.PathData
	// Scanned 是两级条目的展开与存在性扫描结果。
	Scanned *ScannedEntries
	// Dead 是死条目列表。
	Dead []EntryFinding
	// Duplicates 是跨两级重复分组。
	Duplicates []DuplicateGroup
	// Commands 是关键命令的新终端视角解析结果。
	Commands []CommandResolution
	// Npm 是 npm 全局 prefix 体检结果。
	Npm NpmResult
	// TotalEntries 是非空白条目总数。
	TotalEntries int
}

// Check 运行全部检测并汇总。
//
// 变量表 vars 应由两级注册表环境变量经 ComposeVars 合成（新终端视角）；
// npmPrefix 由调用方执行 npm 获取，取不到时传空串（NpmUnavailable）。
func Check(system, user *registry.PathData, vars Vars, pathext []string, commands []string, npmPrefix string) *CheckReport {
	scanned := ScanEntries(system, user, vars)
	return &CheckReport{
		System:       system,
		User:         user,
		Scanned:      scanned,
		Dead:         scanned.DeadEntries(),
		Duplicates:   scanned.DuplicateGroups(),
		Commands:     ResolveCommands(system, user, vars, pathext, commands),
		Npm:          CheckNpmPrefix(npmPrefix, scanned),
		TotalEntries: scanned.Total(),
	}
}

// ProblemCount 返回问题数：死条目数 + 重复组数 + 缺失命令数 + npm prefix 不在 PATH。
// 与 env-check.ps1 口径一致：npm 不可用只警告，不计问题。
func (r *CheckReport) ProblemCount() int {
	n := len(r.Dead) + len(r.Duplicates)
	for _, c := range r.Commands {
		if !c.Found {
			n++
		}
	}
	if r.Npm.Status == NpmNotOnPath {
		n++
	}
	return n
}
