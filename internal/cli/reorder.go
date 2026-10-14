package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"envwarden/core/backup"
	"envwarden/core/mutate"
	"envwarden/core/registry"
	"envwarden/internal/ops"
)

// RunReorder 实现 reorder 子命令：把某作用域 PATH 的第 from 个可见条目移动到第 to 个位置。
//
// PATH 顺序即命令解析优先级（系统级拼在用户级之前、同级内靠前者胜出），
// 这是命令抢占问题的根源，调整顺序是 GUI 拖拽排序的命令行等价物。
// 安全顺序与 clean 一致：计划 → 提权预检 → 确认 → ops 提交流水线。
// 返回值：0 = 成功（含用户取消）；2 = 无法执行。
func RunReorder(args []string) int {
	fs := flag.NewFlagSet("reorder", flag.ContinueOnError)
	scopeFlag := fs.String("scope", "user", "作用域：user / system")
	fromFlag := fs.Int("from", 0, "要移动的条目序号（1 起，按可见条目计，与 check 报告口径一致）")
	toFlag := fs.Int("to", 0, "目标位置序号（1 起，按移动后的列表计）")
	yesFlag := fs.Bool("y", false, "跳过确认提示")
	dryRunFlag := fs.Bool("dry-run", false, "只展示新顺序，不做任何变更")
	dirFlag := fs.String("backup-dir", backup.BackupDirName, "备份文件目录（不存在时自动创建）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	scope, ok := registry.ParseScope(*scopeFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "envwarden reorder: -scope 必须是 user / system（当前值 %q）\n", *scopeFlag)
		return 2
	}
	if *fromFlag < 1 || *toFlag < 1 {
		fmt.Fprintln(os.Stderr, "envwarden reorder: -from 与 -to 必须是 >= 1 的序号")
		return 2
	}

	pd, err := registry.ReadPath(scope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envwarden reorder: %v\n", err)
		return 2
	}
	plan, err := mutate.MoveVisible(pd, *fromFlag, *toFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envwarden reorder: %v\n", err)
		return 2
	}

	newPD, err := registry.NewPathData(scope, plan.NewRaw, plan.Kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envwarden reorder: %v\n", err)
		return 2
	}
	renderReorderPlan(os.Stdout, scope, mutate.VisibleEntries(pd), mutate.VisibleEntries(newPD))
	if *dryRunFlag {
		fmt.Println("（dry-run：以上仅为计划，未做任何变更）")
		return 0
	}

	if scope == registry.ScopeSystem && !registry.IsElevated() {
		fmt.Fprintln(os.Stderr, "envwarden reorder: 系统级 PATH 修改需要管理员权限：请在管理员终端重新运行本命令")
		return 2
	}
	if !*yesFlag && !confirm(os.Stdin, "确认调整以上顺序？[y/N] ") {
		fmt.Println("已取消，未做任何变更。")
		return 0
	}

	res, err := ops.CommitPath(*dirFlag, []ops.PathChange{{
		Scope:  scope,
		OldRaw: pd.Raw,
		NewRaw: plan.NewRaw,
		Kind:   plan.Kind,
	}}, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, ops.ErrPathChanged):
			fmt.Fprintf(os.Stderr, "envwarden reorder: %s级 PATH 在确认期间被外部修改，未做任何变更，请重新运行\n", scopeName(scope))
		case errors.Is(err, registry.ErrAccessDenied):
			fmt.Fprintf(os.Stderr, "envwarden reorder: %s级写入被拒绝，请在管理员终端重新运行: %v\n", scopeName(scope), err)
		default:
			fmt.Fprintf(os.Stderr, "envwarden reorder: %v\n", err)
		}
		return exitWriteFailed(res.Backups)
	}
	for _, p := range res.Backups {
		fmt.Printf("已写入备份: %s\n", p)
	}
	fmt.Printf("%s PATH 顺序已调整（条目原文逐字保留，值类型 %s 不变）\n", scopeName(scope), plan.Kind)
	if res.BroadcastErr != nil {
		fmt.Fprintf(os.Stderr, "警告: %v；新终端可能需注销重登后才能拿到新 PATH\n", res.BroadcastErr)
	}
	fmt.Println("已广播 WM_SETTINGCHANGE。请打开新终端验证 PATH（已开着的旧终端仍持有旧值）。")
	return 0
}

// renderReorderPlan 渲染顺序调整计划：旧顺序 → 新顺序（只列可见条目，空白条目不展示）。
func renderReorderPlan(w io.Writer, scope registry.Scope, oldOrder, newOrder []string) {
	fmt.Fprintf(w, "==== %s级 PATH 顺序调整（命令解析优先级 = 列表顺序） ====\n", scopeName(scope))
	fmt.Fprintf(w, "  旧: %s\n", formatOrder(oldOrder))
	fmt.Fprintf(w, "  新: %s\n", formatOrder(newOrder))
}
