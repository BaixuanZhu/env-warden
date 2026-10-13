package cli

import (
	"bytes"
	"strings"
	"testing"

	"envwarden/core/detect"
	"envwarden/core/registry"
)

func TestRenderCheckReportWithProblems(t *testing.T) {
	rep := &detect.CheckReport{
		TotalEntries: 45,
		Dead: []detect.EntryFinding{
			{Scope: registry.ScopeSystem, Index: 14, Entry: `F:\NodeJS\node_global`},
		},
		Commands: []detect.CommandResolution{
			{Command: "node", Found: true, First: `E:\nvm\.nodejs\node.exe`, FromScope: registry.ScopeUser, FromIndex: 1},
			{Command: "mvn"},
		},
		Npm: detect.NpmResult{Status: detect.NpmOK, Prefix: `C:\Users\zbxComputer\AppData\Roaming\npm`},
	}
	var b bytes.Buffer
	renderCheckReport(&b, rep)
	out := b.String()

	for _, want := range []string{
		"两级共 45 条",
		"[缺失] 系统: F:\\NodeJS\\node_global",
		"==== 2. PATH 重复条目 ====",
		"  无",
		"[OK] node   E:\\nvm\\.nodejs\\node.exe（用户#2）",
		"[缺失] mvn: 不在 PATH 上",
		"[OK] prefix=C:\\Users\\zbxComputer\\AppData\\Roaming\\npm 已在 PATH 上",
		"==> 发现 2 个问题",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染输出缺少 %q\n--- 实际输出 ---\n%s", want, out)
		}
	}
}

func TestRenderCheckReportHealthy(t *testing.T) {
	rep := &detect.CheckReport{
		TotalEntries: 31,
		Commands: []detect.CommandResolution{
			{Command: "node", Found: true, First: `E:\nvm\.nodejs\node.exe`, FromScope: registry.ScopeUser, FromIndex: 1},
		},
		Npm: detect.NpmResult{Status: detect.NpmOK, Prefix: `C:\npm`},
	}
	var b bytes.Buffer
	renderCheckReport(&b, rep)
	out := b.String()

	for _, want := range []string{
		"无死条目",
		"[OK] prefix=C:\\npm 已在 PATH 上",
		"==> 全部通过",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染输出缺少 %q\n--- 实际输出 ---\n%s", want, out)
		}
	}
}

func TestRenderCheckReportNpmUnavailable(t *testing.T) {
	rep := &detect.CheckReport{Npm: detect.NpmResult{Status: detect.NpmUnavailable}}
	var b bytes.Buffer
	renderCheckReport(&b, rep)
	if !strings.Contains(b.String(), "无法确定 npm prefix") {
		t.Errorf("应渲染 npm 不可用警告, got:\n%s", b.String())
	}
}

func TestSplitCommands(t *testing.T) {
	if got := splitCommands(" node , npm ,,  "); len(got) != 2 || got[0] != "node" || got[1] != "npm" {
		t.Errorf("splitCommands: got %v", got)
	}
}
