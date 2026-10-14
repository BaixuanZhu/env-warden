package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"envwarden/core/detect"
	"envwarden/core/registry"
	"envwarden/core/snapshot"
)

// DefaultCommands 是工具链回归的缺省命令清单，CLI 与 GUI 共用。
const DefaultCommands = "node,npm,npx,git,java,mvn"

// RunCheck 实现 check 子命令：读取两级注册表 PATH 与环境变量（新终端视角），
// 运行四类检测并输出中文报告。
// 返回值：0 = 全部通过；1 = 发现问题；2 = 无法完成体检（注册表读取失败等）。
func RunCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	cmdsFlag := fs.String("cmds", DefaultCommands, "工具链回归要解析的命令清单（逗号分隔）")
	snapFlag := fs.Bool("snapshot", true, "体检后保存快照（供 diff 对比；-snapshot=false 跳过）")
	snapDirFlag := fs.String("snapshot-dir", defaultSnapshotDir(), "快照存储目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	system, user, vars, exit := readCheckInputs("check")
	if exit != 0 {
		return exit
	}

	pathext := detect.PathExtFromVars(vars)
	commands := splitCommands(*cmdsFlag)
	rep := detect.Check(system, user, vars, pathext, commands, NpmPrefix())

	renderCheckReport(os.Stdout, rep)
	if *snapFlag {
		path, err := snapshot.Save(*snapDirFlag, snapshot.FromPathData(system, user, time.Now(), "check"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden check: 快照保存失败（不影响体检结果）: %v\n", err)
		} else {
			fmt.Printf("已保存体检快照: %s\n", path)
		}
	}
	if rep.ProblemCount() > 0 {
		return 1
	}
	return 0
}

// splitCommands 拆分逗号分隔的命令清单，去除空白项。
func splitCommands(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NpmPrefix 执行 npm config get prefix 获取全局 prefix；npm 不可用或超时返回空串。
// 查找 npm 走当前进程 PATH（只影响用哪个 npm，prefix 与 PATH 的比对仍是注册表新视角）。
func NpmPrefix() string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "npm", "config", "get", "prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// renderCheckReport 将体检报告渲染为中文文本，四段结构与 env-check.ps1 对齐。
func renderCheckReport(w io.Writer, rep *detect.CheckReport) {
	fmt.Fprintf(w, "==== 1. PATH 死条目检测（两级共 %d 条） ====\n", rep.TotalEntries)
	if len(rep.Dead) == 0 {
		fmt.Fprintln(w, "  无死条目")
	} else {
		for _, d := range rep.Dead {
			fmt.Fprintf(w, "  [缺失] %s: %s\n", scopeName(d.Scope), d.Entry)
		}
	}

	fmt.Fprintln(w, "==== 2. PATH 重复条目 ====")
	if len(rep.Duplicates) == 0 {
		fmt.Fprintln(w, "  无")
	} else {
		for _, g := range rep.Duplicates {
			var locs []string
			for _, m := range g.Members {
				locs = append(locs, fmt.Sprintf("%s#%d", scopeName(m.Scope), m.Index+1))
			}
			fmt.Fprintf(w, "  [重复] %s x%d（%s）\n", g.Normalized, len(g.Members), strings.Join(locs, "、"))
		}
	}

	fmt.Fprintln(w, "==== 3. 工具链回归（新终端视角） ====")
	for _, c := range rep.Commands {
		if !c.Found {
			fmt.Fprintf(w, "  [缺失] %s: 不在 PATH 上\n", c.Command)
			continue
		}
		fmt.Fprintf(w, "  [OK] %-6s %s（%s#%d）\n", c.Command, c.First, scopeName(c.FromScope), c.FromIndex+1)
	}

	fmt.Fprintln(w, "==== 4. npm 全局 prefix 检查 ====")
	switch rep.Npm.Status {
	case detect.NpmOK:
		fmt.Fprintf(w, "  [OK] prefix=%s 已在 PATH 上\n", rep.Npm.Prefix)
	case detect.NpmNotOnPath:
		fmt.Fprintf(w, "  [警告] prefix=%s 不在 PATH 上，npm -g 装的工具将不可用\n", rep.Npm.Prefix)
	default:
		fmt.Fprintln(w, "  [警告] 无法确定 npm prefix（npm 未安装或异常）")
	}

	if n := rep.ProblemCount(); n > 0 {
		fmt.Fprintf(w, "==> 发现 %d 个问题\n", n)
	} else {
		fmt.Fprintln(w, "==> 全部通过")
	}
}

// scopeName 返回作用域中文名。
func scopeName(s registry.Scope) string {
	if s == registry.ScopeSystem {
		return "系统"
	}
	return "用户"
}
