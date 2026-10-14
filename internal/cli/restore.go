package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"envwarden/core/backup"
	"envwarden/core/registry"
	"envwarden/internal/ops"
)

// RunRestore 实现 restore 子命令：把 .reg 备份文件整键写回注册表（按内容自动识别作用域）。
//
// 还原前强制再备份当前值（不可跳过）；只写文件中出现的值，不删除键内其他值；
// 值类型以备份文件为准（还原即回到备份时刻状态，包括当时的 Path 值类型）。
// 接受本工具生成的备份，也接受 regedit 导出的同键 .reg 文件。
// 返回值：0 = 成功（含用户取消）；2 = 无法执行。
func RunRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	yesFlag := fs.Bool("y", false, "跳过确认提示")
	dryRunFlag := fs.Bool("dry-run", false, "只展示将要写回的内容，不做任何变更")
	dirFlag := fs.String("backup-dir", backup.BackupDirName, "还原前自动备份当前值的目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "envwarden restore: 需要恰好一个 .reg 备份文件参数")
		return 2
	}
	file := fs.Arg(0)

	// 预览解析与正式还原共用同一解析器，保证展示与写入一致。
	preview, err := previewRestore(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envwarden restore: %v\n", err)
		return 2
	}
	renderRestorePreview(os.Stdout, preview)
	if *dryRunFlag {
		fmt.Println("（dry-run：以上仅为计划，未做任何变更）")
		return 0
	}

	if preview.Scope == registry.ScopeSystem && !registry.IsElevated() {
		fmt.Fprintln(os.Stderr, "envwarden restore: 系统级还原需要管理员权限：请在管理员终端重新运行本命令")
		return 2
	}
	if !*yesFlag && !confirm(os.Stdin, fmt.Sprintf("确认还原 %s级 Environment？（还原前会自动备份当前值） ", scopeName(preview.Scope))) {
		fmt.Println("已取消，未做任何变更。")
		return 0
	}

	res, err := ops.RestoreFile(*dirFlag, file, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, registry.ErrAccessDenied):
			fmt.Fprintf(os.Stderr, "envwarden restore: %s级写入被拒绝，请在管理员终端重新运行: %v\n", scopeName(res.Scope), err)
		case errors.Is(err, ops.ErrVerifyFailed):
			fmt.Fprintf(os.Stderr, "envwarden restore: %s级写后校验失败（结果与备份不一致）: %v\n", scopeName(res.Scope), err)
		default:
			fmt.Fprintf(os.Stderr, "envwarden restore: %v\n", err)
		}
		return exitWriteFailed(res.Backups)
	}
	for _, p := range res.Backups {
		fmt.Printf("还原前已自动备份当前值: %s\n", p)
	}
	fmt.Printf("%s级 Environment 已还原 %d 个值（类型以备份为准）\n", scopeName(res.Scope), len(res.Written))
	if res.BroadcastErr != nil {
		fmt.Fprintf(os.Stderr, "警告: %v；新终端可能需注销重登后才能拿到新 PATH\n", res.BroadcastErr)
	}
	fmt.Println("已广播 WM_SETTINGCHANGE。请打开新终端验证 PATH（已开着的旧终端仍持有旧值）。")
	return 0
}

// RestorePreview 是还原前的解析预览，供确认前展示与写入使用同一份信息。
type RestorePreview struct {
	// Scope 按文件内容识别出的作用域。
	Scope registry.Scope
	// Values 备份文件中将要写回的值。
	Values []registry.RegValue
}

// previewRestore 解析备份文件并识别作用域，生成还原预览。
func previewRestore(file string) (*RestorePreview, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("读取备份文件: %w", err)
	}
	f, err := registry.ParseRegFile(data)
	if err != nil {
		return nil, err
	}
	scope, values, err := ops.ScopeFromRegFile(f)
	if err != nil {
		return nil, err
	}
	return &RestorePreview{Scope: scope, Values: values}, nil
}

// renderRestorePreview 渲染还原计划：作用域、值清单与每个值的类型。
func renderRestorePreview(w io.Writer, p *RestorePreview) {
	fmt.Fprintf(w, "==== 还原计划（%s级 Environment，从备份整键写回） ====\n", scopeName(p.Scope))
	for _, v := range p.Values {
		fmt.Fprintf(w, "  %s（%s）\n", v.Name, v.Kind)
	}
	fmt.Fprintf(w, "共 %d 个值；未出现在备份中的值保持不变；还原前会自动备份当前值\n", len(p.Values))
}
