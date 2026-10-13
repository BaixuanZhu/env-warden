package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"envwarden/core/registry"
)

// readReg 读取并解析仓库 testdata/ 下的 .reg 夹具。
func readReg(t *testing.T, name string) *registry.RegFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("读取夹具失败: %v", err)
	}
	f, err := registry.ParseRegFile(data)
	if err != nil {
		t.Fatalf("解析夹具 %s 失败: %v", name, err)
	}
	return f
}

// regFileVars 从 .reg 解析结果提取字符串类环境变量表（键大写，值不展开）。
func regFileVars(f *registry.RegFile, scope registry.Scope) map[string]string {
	out := map[string]string{}
	for _, v := range f.Keys[strings.ToLower(scope.KeyPath())] {
		if v.Kind == registry.KindSZ || v.Kind == registry.KindExpandSZ {
			out[strings.ToUpper(v.Name)] = v.Str
		}
	}
	return out
}

// loadFixtures 从 2026-10-13 修复前的 .reg 夹具构建两级 PathData 与变量表。
func loadFixtures(t *testing.T) (system, user *registry.PathData, vars Vars) {
	t.Helper()
	sysFile := readReg(t, "HKLM-Environment.reg")
	usrFile := readReg(t, "HKCU-Environment.reg")

	sv, ok := sysFile.PathValue(registry.ScopeSystem)
	if !ok {
		t.Fatal("夹具缺少系统级 Path")
	}
	uv, ok := usrFile.PathValue(registry.ScopeUser)
	if !ok {
		t.Fatal("夹具缺少用户级 Path")
	}
	system = mustPathData(t, registry.ScopeSystem, sv.Str)
	user = mustPathData(t, registry.ScopeUser, uv.Str)
	vars = ComposeVars(regFileVars(sysFile, registry.ScopeSystem), regFileVars(usrFile, registry.ScopeUser))
	return system, user, vars
}

// TestFixtureDeadEntries 是 M1 验收基线：对修复前夹具跑死条目检测，
// 必须恰好检出 14 个死条目（7 用户 + 7 系统，清单来自清理脚本）且零误报。
// 期望值已于 2026-10-13 逐一核对：14 个目录当日均不存在，存活见证均在位。
func TestFixtureDeadEntries(t *testing.T) {
	system, user, vars := loadFixtures(t)
	dead := ScanEntries(system, user, vars).DeadEntries()

	want := []string{
		// 用户级 7 条（clean-user-path.ps1 的 $dead 清单）
		`C:\Users\zbxComputer\AppData\Local\Temp\ps-hook-test\home\current\bin`,
		`C:\Users\ZBXCOM~1\AppData\Local\Temp\jvmtest\home\current\bin`,
		`C:\Users\zbxComputer\go\bin`,
		`C:\Users\zbxComputer\AppData\Local\Programs\OpenAI\Codex\bin`,
		`C:\Users\zbxComputer\AppData\Local\UniGetUI\Chocolatey\bin`,
		`I:\Cpolar`,
		`I:\IntelliJ IDEA 2022.1.31\bin`,
		// 系统级 7 条（clean-system-path.ps1 的 $dead 清单）
		`F:\Downloads\apache-jmeter-5.6.2\bin`,
		`F:\微信web开发者工具\dll`,
		`E:\andoidSDK\platform-tools`,
		`E:\Tesseract-OCR`,
		`C:\ProgramData\chocolatey\bin`,
		`F:\NodeJS\node_global`,
		`D:\BtSoft\panel\script`,
	}
	if len(dead) != len(want) {
		for _, d := range dead {
			t.Logf("实际检出: %s#%d %s", d.Scope, d.Index, d.Entry)
		}
		t.Fatalf("死条目数: got %d, want %d", len(dead), len(want))
	}
	gotSet := map[string]bool{}
	for _, d := range dead {
		gotSet[NormalizeEntry(d.Entry)] = true
	}
	for _, w := range want {
		if !gotSet[NormalizeEntry(w)] {
			t.Errorf("期望检出的死条目未检出: %s", w)
		}
	}
	// 存活见证：清理脚本刻意保留、当日确认存在的条目不得误报。
	for _, alive := range []string{
		`C:\Windows\dll`,
		`E:\nvm\.nodejs`,
		`F:\JetBrains\WebStorm 2022.1.3\bin`,
	} {
		if gotSet[NormalizeEntry(alive)] {
			t.Errorf("存活条目被误报为死条目: %s", alive)
		}
	}
}

// TestFixtureNoDuplicates 修复前夹具中无重复条目（数据事实，作为重复检测的零误报见证）。
func TestFixtureNoDuplicates(t *testing.T) {
	system, user, vars := loadFixtures(t)
	for _, g := range ScanEntries(system, user, vars).DuplicateGroups() {
		t.Errorf("夹具中检出重复: %s x%d", g.Normalized, len(g.Members))
	}
}

// TestFixtureNpmPrefixOnPath 事故修复后的 npm prefix（C 盘 Roaming\npm）
// 在修复前夹具中已位于系统级 PATH —— 事故根源是 .npmrc 的 prefix 指向 F 盘，
// 本用例验证"prefix 在 PATH 上"判定的正路。
func TestFixtureNpmPrefixOnPath(t *testing.T) {
	system, user, vars := loadFixtures(t)
	scanned := ScanEntries(system, user, vars)
	res := CheckNpmPrefix(`C:\Users\zbxComputer\AppData\Roaming\npm`, scanned)
	if res.Status != NpmOK {
		t.Errorf("Roaming\\npm 应命中夹具系统级 PATH: got %d", res.Status)
	}
}
