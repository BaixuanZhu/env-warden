// Command envwarden 是 Windows 环境变量体检、诊断与安全清理的命令行入口。
//
// 子命令：check（体检）、backup（备份）、clean（安全清理）、diff（快照对比）。
// 当前为项目骨架阶段，各子命令尚未实现，仅完成入口分发与用法说明。
package main

import (
	"fmt"
	"os"
)

// version 为当前构建版本，骨架阶段为占位值。
const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-v", "--version":
		fmt.Println("envwarden", version)
	case "check", "backup", "clean", "diff":
		fmt.Fprintf(os.Stderr, "envwarden: 子命令 %q 尚未实现（版本 %s，骨架阶段）\n", os.Args[1], version)
		os.Exit(1)
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
  envwarden backup             导出两级 Environment 键为 .reg 备份
  envwarden clean              精确清理死条目（变更前自动备份）
  envwarden diff               对比两次体检快照
  envwarden -v | --version     显示版本

各子命令实现中，当前为项目骨架。
`, version)
}
