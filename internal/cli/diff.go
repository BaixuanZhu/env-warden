package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"envwarden/core/snapshot"
)

// RunDiff 实现 diff 子命令：对比"上次体检以来 PATH 变了什么"。
//
// 参数形式：无参 = 最新快照 vs 当前注册表；一个参数 = 指定快照文件 vs 当前注册表；
// 两个参数 = 两个快照文件互比。
// 返回值：0 = 无变化；1 = 发现变化；2 = 无法执行（没有快照、文件损坏、读取失败等）。
func RunDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	snapDirFlag := fs.String("snapshot-dir", defaultSnapshotDir(), "快照存储目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 2 {
		fmt.Fprintf(os.Stderr, "envwarden diff: 至多两个快照文件参数\n")
		return 2
	}

	var oldSnap, newSnap *snapshot.Snapshot
	var oldLabel, newLabel string
	switch fs.NArg() {
	case 0:
		s, path, err := snapshot.Latest(*snapDirFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden diff: %v（先运行 envwarden check 生成快照）\n", err)
			return 2
		}
		oldSnap, oldLabel = s, path
	case 1:
		s, err := snapshot.Load(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden diff: %v\n", err)
			return 2
		}
		oldSnap, oldLabel = s, fs.Arg(0)
	case 2:
		s, err := snapshot.Load(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden diff: %v\n", err)
			return 2
		}
		oldSnap, oldLabel = s, fs.Arg(0)
		s, err = snapshot.Load(fs.Arg(1))
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden diff: %v\n", err)
			return 2
		}
		newSnap, newLabel = s, fs.Arg(1)
	}
	if newSnap == nil {
		cur, err := snapshot.Capture(time.Now(), "diff-current")
		if err != nil {
			fmt.Fprintf(os.Stderr, "envwarden diff: %v\n", err)
			return 2
		}
		newSnap, newLabel = cur, "当前注册表"
	}

	rep := snapshot.Diff(oldSnap, newSnap)
	renderDiffReport(os.Stdout, rep, oldLabel, newLabel)
	if rep.ChangeCount() > 0 {
		return 1
	}
	return 0
}

// renderDiffReport 将快照对比结果渲染为中文文本：分作用域列出新增 / 删除 / 顺序 / 类型变化。
func renderDiffReport(w io.Writer, rep *snapshot.DiffReport, oldLabel, newLabel string) {
	fmt.Fprintln(w, "==== PATH 变化对比 ====")
	fmt.Fprintf(w, "基准: %s（%s）\n", oldLabel, rep.OldAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "对比: %s（%s）\n", newLabel, rep.NewAt.Format("2006-01-02 15:04:05"))
	for _, s := range rep.Scopes {
		fmt.Fprintf(w, "%s级 PATH:\n", scopeName(s.Scope))
		if len(s.Added) == 0 && len(s.Removed) == 0 && !s.OrderChanged && !s.KindChanged {
			fmt.Fprintln(w, "  无变化")
			continue
		}
		for _, a := range s.Added {
			fmt.Fprintf(w, "  [新增] #%d %s\n", a.Position, a.Entry)
		}
		for _, r := range s.Removed {
			fmt.Fprintf(w, "  [删除] #%d %s\n", r.Position, r.Entry)
		}
		if s.OrderChanged {
			fmt.Fprintln(w, "  [顺序] 命令解析优先级变化：")
			fmt.Fprintf(w, "    旧: %s\n", formatOrder(s.OldOrder))
			fmt.Fprintf(w, "    新: %s\n", formatOrder(s.NewOrder))
		}
		if s.KindChanged {
			fmt.Fprintf(w, "  [类型] 值类型变化: %s → %s（%%VAR%% 展开行为已改变）\n", s.OldKind, s.NewKind)
		}
	}
	if n := rep.ChangeCount(); n > 0 {
		fmt.Fprintf(w, "==> 共 %d 处变化\n", n)
	} else {
		fmt.Fprintln(w, "==> 无变化")
	}
}

// formatOrder 把条目序列渲染为"1.条目 → 2.条目"的优先级顺序。
func formatOrder(entries []string) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%d.%s", i+1, e)
	}
	return strings.Join(parts, " → ")
}

// defaultSnapshotDir 返回快照默认目录 %LOCALAPPDATA%\envwarden\snapshots；
// LOCALAPPDATA 缺失时回退到当前目录下，保证工具始终可用。
func defaultSnapshotDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return snapshot.DirName
	}
	return filepath.Join(base, "envwarden", "snapshots")
}
