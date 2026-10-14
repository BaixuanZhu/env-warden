package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"envwarden/core/backup"
	"envwarden/core/registry"
)

// RunBackup 实现 backup 子命令：导出两级（或指定一级）Environment 键为
// regedit 5.00 格式的 .reg 备份文件，还原方式为双击导入。
// 返回值：0 = 成功；2 = 无法完成（读取注册表或写文件失败）。
func RunBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	scopeFlag := fs.String("scope", "all", "备份作用域：user / system / all")
	dirFlag := fs.String("o", backup.BackupDirName, "备份文件目录（不存在时自动创建）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	scopes, ok := parseScopes(*scopeFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "envwarden backup: -scope 必须是 user / system / all（当前值 %q）\n", *scopeFlag)
		return 2
	}

	now := time.Now()
	for _, s := range scopes {
		path, err := backup.WriteScopeFile(*dirFlag, s, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden backup: %v\n", err)
			return 2
		}
		fmt.Printf("已备份 %s级 Environment → %s\n", scopeName(s), path)
	}
	fmt.Println("还原方式：双击 .reg 文件导入（系统级导入时 regedit 会自动请求管理员权限）")
	return 0
}

// parseScopes 将 -scope 参数解析为作用域列表；all = 用户 + 系统（用户在前，与报告顺序一致）。
// 第二个返回值表示参数是否合法。
func parseScopes(s string) ([]registry.Scope, bool) {
	switch s {
	case "user":
		return []registry.Scope{registry.ScopeUser}, true
	case "system":
		return []registry.Scope{registry.ScopeSystem}, true
	case "all":
		return []registry.Scope{registry.ScopeUser, registry.ScopeSystem}, true
	default:
		return nil, false
	}
}
