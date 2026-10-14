package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"envwarden/core/backup"
	"envwarden/core/broadcast"
	"envwarden/core/detect"
	"envwarden/core/mutate"
	"envwarden/core/registry"
)

// RunClean 实现 clean 子命令：检测死条目（可叠加手写条目），
// 经确认后强制备份、精确移除、类型保持写回并广播变更。
//
// 安全顺序固定且不可跳过：计划 → 提权预检 → 确认 → 变更前复检 → 强制备份 → 写入 → 写后校验 → 广播。
// 返回值：0 = 成功（含无可清理与用户取消）；2 = 无法执行（读取失败、目标未命中、未提权等）。
func RunClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	scopeFlag := fs.String("scope", "all", "清理作用域：user / system / all")
	var entries entryList
	fs.Var(&entries, "entry", "要移除的条目原文（与 check 报告逐字一致，可多次指定）；默认只清理死条目")
	yesFlag := fs.Bool("y", false, "跳过确认提示")
	dryRunFlag := fs.Bool("dry-run", false, "只展示将要移除的条目，不写任何变更")
	dirFlag := fs.String("backup-dir", backup.BackupDirName, "备份文件目录（不存在时自动创建）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	scopes, ok := parseScopes(*scopeFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "envwarden clean: -scope 必须是 user / system / all（当前值 %q）\n", *scopeFlag)
		return 2
	}

	// 体检链输入：两级 PATH 与全部环境变量（新终端视角）。
	system, user, vars, exit := readCheckInputs("clean")
	if exit != 0 {
		return exit
	}
	scanned := detect.ScanEntries(system, user, vars)

	// 组装各作用域的移除目标：死条目原文 + 手写条目（按作用域定位）。
	targets := map[registry.Scope][]string{}
	for _, d := range scanned.DeadEntries() {
		targets[d.Scope] = append(targets[d.Scope], d.Entry)
	}
	if len(entries) > 0 {
		locatedAny := map[string]bool{}
		for _, s := range scopes {
			pd := system
			if s == registry.ScopeUser {
				pd = user
			}
			found, _ := mutate.LocateExact(pd.Raw, entries)
			for _, e := range found {
				locatedAny[e] = true
			}
			targets[s] = append(targets[s], found...)
		}
		var missing []string
		for _, e := range entries {
			if !locatedAny[e] {
				missing = append(missing, e)
			}
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "envwarden clean: 以下 -entry 条目在 %s 级 PATH 中逐字不存在（须与 check 报告原文一致）：\n", *scopeFlag)
			for _, m := range missing {
				fmt.Fprintf(os.Stderr, "  %q\n", m)
			}
			return 2
		}
	}

	// 生成精确移除计划；任一目标未命中即中止（不写入任何变更）。
	var plans []*mutate.RemovalPlan
	for _, s := range scopes {
		if len(targets[s]) == 0 {
			continue
		}
		pd := system
		if s == registry.ScopeUser {
			pd = user
		}
		plan, err := mutate.RemoveExact(pd, targets[s])
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden clean: %v\n", err)
			return 2
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		fmt.Println("未发现死条目，无需清理。")
		return 0
	}

	renderCleanPlan(os.Stdout, plans)
	if *dryRunFlag {
		fmt.Println("（dry-run：以上仅为计划，未做任何变更）")
		return 0
	}

	// 提权预检：系统级有变更而未提权时明确报错，不做任何操作。
	for _, p := range plans {
		if p.Scope == registry.ScopeSystem && !registry.IsElevated() {
			fmt.Fprintln(os.Stderr, "envwarden clean: 系统级 PATH 修改需要管理员权限：请在管理员终端重新运行本命令")
			return 2
		}
	}

	if !*yesFlag && !confirm(os.Stdin, "确认清理以上条目？[y/N] ") {
		fmt.Println("已取消，未做任何变更。")
		return 0
	}

	// 变更前复检：确认期间 PATH 被外部修改则放弃，避免基于旧快照覆盖新变更。
	for _, p := range plans {
		pd := system
		if p.Scope == registry.ScopeUser {
			pd = user
		}
		cur, err := registry.ReadPath(p.Scope)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden clean: 复检失败: %v\n", err)
			return 2
		}
		if cur.Raw != pd.Raw {
			fmt.Fprintf(os.Stderr, "envwarden clean: %s级 PATH 在确认期间被外部修改，未做任何变更，请重新运行\n", scopeName(p.Scope))
			return 2
		}
	}

	// 强制备份（不可跳过）：仅备份将被修改的作用域。
	now := time.Now()
	var backupPaths []string
	for _, p := range plans {
		path, err := backup.WriteScopeFile(*dirFlag, p.Scope, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden clean: 备份失败，已中止（未做任何变更）: %v\n", err)
			return 2
		}
		backupPaths = append(backupPaths, path)
		fmt.Printf("已写入备份: %s\n", path)
	}

	// 写入并逐作用域校验：条目与值类型必须与计划一致，否则立即报错并指向备份。
	for _, p := range plans {
		newPD, err := registry.NewPathData(p.Scope, p.NewRaw, p.Kind)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden clean: %v\n", err)
			return exitWriteFailed(backupPaths)
		}
		if err := registry.WritePath(newPD); err != nil {
			if errors.Is(err, registry.ErrAccessDenied) {
				fmt.Fprintf(os.Stderr, "envwarden clean: %s级写入被拒绝，请在管理员终端重新运行: %v\n", scopeName(p.Scope), err)
			} else {
				fmt.Fprintf(os.Stderr, "envwarden clean: %s级写入失败: %v\n", scopeName(p.Scope), err)
			}
			return exitWriteFailed(backupPaths)
		}
		cur, err := registry.ReadPath(p.Scope)
		if err != nil || cur.Raw != p.NewRaw || cur.Kind != p.Kind {
			fmt.Fprintf(os.Stderr, "envwarden clean: %s级写后校验失败（结果与计划不一致）: %v\n", scopeName(p.Scope), err)
			return exitWriteFailed(backupPaths)
		}
		fmt.Printf("%s PATH: 移除 %d 条，保留 %d 条（值类型 %s 保持不变）\n",
			scopeName(p.Scope), len(p.Removed), len(p.Kept), p.Kind)
	}

	// 广播失败不回滚（变更已生效），仅警告。
	if err := broadcast.NotifyEnvironmentChange(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: %v；新终端可能需注销重登后才能拿到新 PATH\n", err)
	}
	fmt.Println("已广播 WM_SETTINGCHANGE。请打开新终端验证 PATH（已开着的旧终端仍持有旧值）。")
	return 0
}

// readCheckInputs 一次性读取体检链所需的注册表输入：两级 PATH 与全部环境变量。
// 出错时打印信息并返回非零退出码；成功时返回 0。
func readCheckInputs(cmd string) (system, user *registry.PathData, vars detect.Vars, exit int) {
	system, err := registry.ReadPath(registry.ScopeSystem)
	if err == nil {
		user, err = registry.ReadPath(registry.ScopeUser)
	}
	if err == nil {
		var sysVars, usrVars map[string]string
		sysVars, err = registry.ReadStringValues(registry.ScopeSystem)
		if err == nil {
			usrVars, err = registry.ReadStringValues(registry.ScopeUser)
		}
		if err == nil {
			return system, user, detect.ComposeVars(sysVars, usrVars), 0
		}
	}
	fmt.Fprintf(os.Stderr, "envwarden %s: %v\n", cmd, err)
	return nil, nil, nil, 2
}

// renderCleanPlan 渲染清理计划：待移除条目原文与各作用域的保留统计。
func renderCleanPlan(w io.Writer, plans []*mutate.RemovalPlan) {
	fmt.Fprintln(w, "==== 待移除条目（逐字精确匹配，其余条目逐字保留） ====")
	for _, p := range plans {
		for _, e := range p.Removed {
			fmt.Fprintf(w, "  [%s] %s\n", scopeName(p.Scope), e)
		}
	}
	for _, p := range plans {
		fmt.Fprintf(w, "%s PATH: 移除 %d 条，保留 %d 条，值类型 %s 保持不变\n",
			scopeName(p.Scope), len(p.Removed), len(p.Kept), p.Kind)
	}
}

// confirm 在 r 上读取一行，仅接受 y / Y 为确认；空输入（如非交互管道）视为取消。
func confirm(r io.Reader, prompt string) bool {
	fmt.Print(prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.TrimSpace(line)
	return answer == "y" || answer == "Y"
}

// exitWriteFailed 统一处理写入失败后的提示与退出码。
func exitWriteFailed(backupPaths []string) int {
	if len(backupPaths) > 0 {
		fmt.Fprintln(os.Stderr, "如需还原，双击导入以下备份文件：")
		for _, p := range backupPaths {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
	}
	return 2
}

// entryList 收集可重复的 -entry 参数值；路径可含逗号，故不做逗号拆分，每处一次取一个条目原文。
type entryList []string

// String 实现 flag.Value，输出逗号连接的当前值。
func (e *entryList) String() string {
	return strings.Join(*e, ",")
}

// Set 追加一个条目原文，实现 flag.Value 以支持 -entry 多次指定；空值视为参数错误。
func (e *entryList) Set(v string) error {
	if v == "" {
		return errors.New("-entry 不能为空")
	}
	*e = append(*e, v)
	return nil
}
