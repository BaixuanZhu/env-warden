package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"envwarden/core/snapshot"
)

// buildDiffReport 构造带用户级变化的对比结果，供渲染断言。
func buildDiffReport() *snapshot.DiffReport {
	at := time.Date(2026, 10, 14, 20, 41, 44, 0, time.Local)
	now := time.Date(2026, 10, 14, 21, 0, 5, 0, time.Local)
	return snapshot.Diff(
		&snapshot.Snapshot{Version: 1, CreatedAt: at, Source: "check", Paths: []snapshot.PathSnapshot{
			{Scope: "user", Raw: `C:\a;C:\old;C:\b;C:\c`, Kind: "REG_SZ"},
			{Scope: "system", Raw: `C:\w`, Kind: "REG_SZ"},
		}},
		&snapshot.Snapshot{Version: 1, CreatedAt: now, Source: "diff-current", Paths: []snapshot.PathSnapshot{
			{Scope: "user", Raw: `C:\b;C:\a;C:\new;C:\c`, Kind: "REG_EXPAND_SZ"},
			{Scope: "system", Raw: `C:\w`, Kind: "REG_SZ"},
		}},
	)
}

func TestRenderDiffReport(t *testing.T) {
	var buf bytes.Buffer
	renderDiffReport(&buf, buildDiffReport(), "snap-old.json", "当前注册表")
	out := buf.String()
	for _, want := range []string{
		"==== PATH 变化对比 ====",
		"基准: snap-old.json（2026-10-14 20:41:44）",
		"对比: 当前注册表（2026-10-14 21:00:05）",
		"  [新增] #3 C:\\new",
		"  [删除] #2 C:\\old",
		"  [顺序] 命令解析优先级变化：",
		"    旧: 1.C:\\a → 2.C:\\b → 3.C:\\c",
		"    新: 1.C:\\b → 2.C:\\a → 3.C:\\c",
		"  [类型] 值类型变化: REG_SZ → REG_EXPAND_SZ（%VAR% 展开行为已改变）",
		"系统级 PATH:",
		"  无变化",
		"==> 共 4 处变化",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestRenderDiffReportNoChange(t *testing.T) {
	at := time.Date(2026, 10, 14, 20, 41, 44, 0, time.Local)
	rep := snapshot.Diff(
		&snapshot.Snapshot{Version: 1, CreatedAt: at, Paths: []snapshot.PathSnapshot{
			{Scope: "user", Raw: `C:\a`, Kind: "REG_SZ"},
			{Scope: "system", Raw: `C:\w`, Kind: "REG_SZ"},
		}},
		&snapshot.Snapshot{Version: 1, CreatedAt: at, Paths: []snapshot.PathSnapshot{
			{Scope: "user", Raw: `C:\a;`, Kind: "REG_SZ"},
			{Scope: "system", Raw: `C:\w`, Kind: "REG_SZ"},
		}},
	)
	var buf bytes.Buffer
	renderDiffReport(&buf, rep, "a.json", "b.json")
	out := buf.String()
	if !strings.Contains(out, "==> 无变化") {
		t.Errorf("应报告无变化:\n%s", out)
	}
	if rep.ChangeCount() != 0 {
		t.Errorf("ChangeCount = %d, want 0", rep.ChangeCount())
	}
}

// TestDefaultSnapshotDirDegraded 校验 LOCALAPPDATA 缺失时的回退路径。
func TestDefaultSnapshotDirDegraded(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	if got := defaultSnapshotDir(); got != snapshot.DirName {
		t.Errorf("回退目录 = %q, want %q", got, snapshot.DirName)
	}
}
