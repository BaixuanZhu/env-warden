package detect

import (
	"os"
	"path/filepath"
	"testing"

	"envwarden/core/registry"
)

// writeFile 在 dir 下创建名为 name 的空文件。
func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCommandsOrderAndPathExt(t *testing.T) {
	sysDir, usrDir := t.TempDir(), t.TempDir()
	writeFile(t, sysDir, "node.exe")
	writeFile(t, usrDir, "node.exe")
	writeFile(t, usrDir, "npm.cmd")

	sys := mustPathData(t, registry.ScopeSystem, sysDir)
	usr := mustPathData(t, registry.ScopeUser, usrDir)
	pathext := []string{".COM", ".EXE", ".BAT", ".CMD"}

	res := map[string]CommandResolution{}
	for _, r := range ResolveCommands(sys, usr, nil, pathext, []string{"node", "npm", "java"}) {
		res[r.Command] = r
	}

	node := res["node"]
	if !node.Found || node.First != filepath.Join(sysDir, "node.exe") {
		t.Errorf("node 第一命中应在系统级目录: %+v", node)
	}
	if node.FromScope != registry.ScopeSystem {
		t.Errorf("node 命中作用域: got %s, want system", node.FromScope)
	}
	if len(node.All) != 2 || node.All[0] != filepath.Join(sysDir, "node.exe") {
		t.Errorf("node 全部命中应含两级且系统级在前: %v", node.All)
	}

	npm := res["npm"]
	if !npm.Found || npm.First != filepath.Join(usrDir, "npm.cmd") {
		t.Errorf("npm 应经 PATHEXT 找到 npm.cmd: %+v", npm)
	}

	if res["java"].Found {
		t.Error("java 不存在时不应命中")
	}
}

func TestResolveCommandsVarEntry(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, tmp, "tool.exe")
	usr := mustPathData(t, registry.ScopeUser, `%BASE%`)
	res := ResolveCommands(nil, usr, Vars{"BASE": tmp}, []string{".EXE"}, []string{"tool"})
	if !res[0].Found || res[0].First != filepath.Join(tmp, "tool.exe") {
		t.Errorf("%%BASE%% 条目内的命令应可解析: %+v", res[0])
	}
}

func TestResolveCommandsSkipsEmptyEntry(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, tmp, "x.exe")
	usr := mustPathData(t, registry.ScopeUser, `;`+tmp+`;`)
	res := ResolveCommands(nil, usr, nil, []string{".EXE"}, []string{"x"})
	if !res[0].Found {
		t.Error("空条目应被跳过且不影响后续目录解析")
	}
}

func TestPathExtFromVars(t *testing.T) {
	if got := PathExtFromVars(Vars{"PATHEXT": ".COM;.EXE;.JS"}); len(got) != 3 || got[2] != ".JS" {
		t.Errorf("PATHEXT 拆分: got %v", got)
	}
	if got := PathExtFromVars(nil); len(got) != 4 || got[0] != ".COM" || got[3] != ".CMD" {
		t.Errorf("缺省 PATHEXT: got %v", got)
	}
	if got := PathExtFromVars(Vars{"PATHEXT": "PY;.PYW"}); len(got) != 2 || got[0] != ".PY" {
		t.Errorf("无前导点应补齐: got %v", got)
	}
}
