package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"envwarden/core/registry"
	"envwarden/internal/ops"
)

func TestRenderReorderPlan(t *testing.T) {
	var b bytes.Buffer
	renderReorderPlan(&b, registry.ScopeUser,
		[]string{`C:\a`, `C:\b`, `C:\c`},
		[]string{`C:\b`, `C:\c`, `C:\a`})
	out := b.String()
	for _, want := range []string{
		"用户级 PATH 顺序调整",
		"旧: 1.C:\\a → 2.C:\\b → 3.C:\\c",
		"新: 1.C:\\b → 2.C:\\c → 3.C:\\a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestPreviewRestore(t *testing.T) {
	dir := t.TempDir()
	// 用真实夹具做预览：识别作用域 + 值清单。
	file := filepath.Join("..", "..", "testdata", "HKCU-Environment.reg")
	p, err := previewRestore(file)
	if err != nil {
		t.Fatalf("previewRestore: %v", err)
	}
	if p.Scope != registry.ScopeUser {
		t.Errorf("Scope = %s, want user", p.Scope)
	}
	var names []string
	for _, v := range p.Values {
		names = append(names, v.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"Path", "TEMP", "TMP"} {
		if !strings.Contains(joined, want) {
			t.Errorf("值清单缺少 %s: %v", want, names)
		}
	}

	var b bytes.Buffer
	renderRestorePreview(&b, p)
	out := b.String()
	if !strings.Contains(out, "用户级 Environment") || !strings.Contains(out, "Path（") {
		t.Errorf("预览输出缺少关键信息:\n%s", out)
	}

	t.Run("不存在的文件", func(t *testing.T) {
		if _, err := previewRestore(filepath.Join(dir, "no.reg")); err == nil {
			t.Error("应报错")
		}
	})
	t.Run("非 Environment 键", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.reg")
		// UTF-8 无 BOM 的 .reg 也被支持，便于手写构造。
		content := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\X]\r\n\"A\"=\"1\"\r\n"
		if err := os.WriteFile(bad, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := previewRestore(bad)
		if err == nil || !strings.Contains(err.Error(), "Environment") {
			t.Errorf("err = %v, want ErrUnknownScopeKey 提示", err)
		}
	})
}

// TestScopeFromRegFileExported 确认 CLI 预览与 ops 还原共用同一识别逻辑（编译期引用）。
var _ = ops.ScopeFromRegFile
