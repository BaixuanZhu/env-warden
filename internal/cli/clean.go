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
	"envwarden/core/detect"
	"envwarden/core/mutate"
	"envwarden/core/registry"
	"envwarden/internal/ops"
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

	// 提交流水线（复检 → 强制备份 → 写入 → 校验 → 广播）由 internal/ops 统一实现。
	changes := make([]ops.PathChange, len(plans))
	for i, p := range plans {
		pd := system
		if p.Scope == registry.ScopeUser {
			pd = user
		}
		changes[i] = ops.PathChange{Scope: p.Scope, OldRaw: pd.Raw, NewRaw: p.NewRaw, Kind: p.Kind}
	}
	res, err := ops.CommitPath(*dirFlag, changes, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, ops.ErrPathChanged):
			fmt.Fprintf(os.Stderr, "envwarden clean: %s级 PATH 在确认期间被外部修改，未做任何变更，请重新运行\n", scopeOfChanges(changes))
		case errors.Is(err, registry.ErrAccessDenied):
			fmt.Fprintf(os.Stderr, "envwarden clean: %s级写入被拒绝，请在管理员终端重新运行: %v\n", scopeOfChanges(changes), err)
		default:
			fmt.Fprintf(os.Stderr, "envwarden clean: %v\n", err)
		}
		return exitWriteFailed(res.Backups)
	}
	for _, p := range res.Backups {
		fmt.Printf("已写入备份: %s\n", p)
	}
	for _, p := range plans {
		fmt.Printf("%s PATH: 移除 %d 条，保留 %d 条（值类型 %s 保持不变）\n",
			scopeName(p.Scope), len(p.Removed), len(p.Kept), p.Kind)
	}

	if res.BroadcastErr != nil {
		fmt.Fprintf(os.Stderr, "警告: %v；新终端可能需注销重登后才能拿到新 PATH\n", res.BroadcastErr)
	}
	fmt.Println("已广播 WM_SETTINGCHANGE。请打开新终端验证 PATH（已开着的旧终端仍持有旧值）。")
	return 0
}

// scopeOfChanges 汇总变更涉及的作用域中文名，用于错误提示。
func scopeOfChanges(changes []ops.PathChange) string {
	var names []string
	for _, c := range changes {
		names = append(names, scopeName(c.Scope))
	}
	return strings.Join(names, "、")
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
