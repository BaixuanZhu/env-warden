// Command envwarden 是 Windows 环境变量体检、诊断与安全清理的命令行入口。
//
// 子命令：check（体检，自动存快照）、backup（备份）、clean（安全清理）、diff（快照对比）。
package main

import (
	"fmt"
	"os"

	"envwarden/internal/cli"
)

// version 为当前构建版本。
const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-v", "--version":
		fmt.Println("envwarden", version)
	case "check":
		os.Exit(cli.RunCheck(os.Args[2:]))
	case "backup":
		os.Exit(cli.RunBackup(os.Args[2:]))
	case "clean":
		os.Exit(cli.RunClean(os.Args[2:]))
	case "diff":
		os.Exit(cli.RunDiff(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

// usage 输出 CLI 用法摘要到标准错误流。
func usage() {
	fmt.Fprintf(os.Stderr, `envwarden %s — Windows 环境变量体检、诊断与安全清理

用法:
  envwarden check              PATH 体检（死条目 / 重复 / 命令解析 / npm prefix）
  envwarden check -cmds a,b,c  自定义工具链回归清单
  envwarden backup             导出两级 Environment 键为 .reg 备份（还原 = 双击导入）
  envwarden backup -scope user -o 备份目录
  envwarden clean              精确清理死条目（强制先备份，其余条目逐字保留）
  envwarden clean -dry-run     只展示计划，不做任何变更
  envwarden clean -entry "F:\死路径\bin"   叠加手写条目（可多次指定，须与 check 报告逐字一致）
  envwarden clean -scope system -y         系统级清理需在管理员终端运行
  envwarden diff               上次体检快照 vs 当前注册表（PATH 变了什么）
  envwarden diff 旧.json 新.json           两个快照文件互比
  envwarden -v | --version     显示版本

退出码：check：0 = 通过，1 = 发现问题，2 = 无法执行；
       backup / clean：0 = 成功，2 = 无法执行；
       diff：0 = 无变化，1 = 发现变化，2 = 无法执行。
check 每次运行自动保存快照到 %%LOCALAPPDATA%%\envwarden\snapshots。
`, version)
}
