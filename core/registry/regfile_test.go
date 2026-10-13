package registry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// encodeUTF16LE 将文本编码为带 BOM 的 UTF-16LE 字节序列（regedit 5.00 格式）。
func encodeUTF16LE(text string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, u := range utf16.Encode([]rune(text)) {
		buf.WriteByte(byte(u))
		buf.WriteByte(byte(u >> 8))
	}
	return buf.Bytes()
}

// hexLine 将字符串编码为 "xx,xx,..." 形式的 UTF-16LE 十六进制序列（regedit hex(2) 语法）。
func hexLine(s string) string {
	var sb strings.Builder
	for i, u := range utf16.Encode([]rune(s)) {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%02x,%02x", byte(u), byte(u>>8))
	}
	return sb.String()
}

func TestParseRegFileSynthetic(t *testing.T) {
	// TEMP 用 hex(2) 且跨续行，覆盖 %VAR% 不展开 + NUL 终止符 + 续行合并。
	hexParts := strings.Split(hexLine(`%USERPROFILE%\AppData\Local\Temp`+"\x00"), ",")
	mid := len(hexParts) / 2
	line1 := strings.Join(hexParts[:mid], ",") + ","
	line2 := strings.Join(hexParts[mid:], ",")

	content := "Windows Registry Editor Version 5.00\r\n" +
		"\r\n" +
		"[HKEY_CURRENT_USER\\Environment]\r\n" +
		"\"Path\"=\"C:\\\\a;D:\\\\b\"\r\n" +
		"\"TEMP\"=hex(2):" + line1 + "\\\r\n" +
		"  " + line2 + "\r\n" +
		"\"Escaped\"=\"a\\\\b\\\"c\"\r\n" +
		"\"RefCount\"=dword:0000002a\r\n" +
		"[HKEY_CURRENT_USER\\Other]\r\n" +
		"\"Bin\"=hex:01,02,ff\r\n"

	f, err := ParseRegFile(encodeUTF16LE(content))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	v, ok := f.PathValue(ScopeUser)
	if !ok {
		t.Fatal("未找到用户级 Path 值")
	}
	if v.Kind != KindSZ {
		t.Errorf("Path 类型: got %s, want REG_SZ", v.Kind)
	}
	if v.Str != `C:\a;D:\b` {
		t.Errorf("Path 值: got %q, want %q", v.Str, `C:\a;D:\b`)
	}

	tmp, ok := f.Value(ScopeUser.KeyPath(), "temp") // 值名不区分大小写
	if !ok {
		t.Fatal("未找到 TEMP 值")
	}
	if tmp.Kind != KindExpandSZ {
		t.Errorf("TEMP 类型: got %s, want REG_EXPAND_SZ", tmp.Kind)
	}
	if tmp.Str != `%USERPROFILE%\AppData\Local\Temp` {
		t.Errorf("TEMP 值（NUL 应被去除、%%VAR%% 不展开）: got %q", tmp.Str)
	}

	esc, ok := f.Value(ScopeUser.KeyPath(), "Escaped")
	if !ok || esc.Str != `a\b"c` {
		t.Errorf("转义字符串: got %+v, want a\\b\"c", esc)
	}

	dw, ok := f.Value(ScopeUser.KeyPath(), "RefCount")
	if !ok || dw.Kind != KindDWORD || dw.Dword != 42 {
		t.Errorf("dword: got %+v, want 42", dw)
	}

	// 键路径查找不区分大小写。
	if _, ok := f.Value("hkey_current_user\\environment", "Path"); !ok {
		t.Error("小写键路径应能命中")
	}
	if b, ok := f.Value(`HKEY_CURRENT_USER\Other`, "Bin"); !ok || b.Kind != KindBinary || len(b.Raw) != 3 {
		t.Errorf("hex: got %+v", b)
	}
}

func TestParseRegFileRejectsGarbage(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"非 regedit 内容", []byte("hello world")},
		{"空文件", nil},
		{"UTF-16BE BOM", []byte{0xFE, 0xFF, 0x00, 0x41}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseRegFile(tt.data); err == nil {
				t.Fatal("期望解析失败，却成功了")
			}
		})
	}
}

// loadFixture 读取仓库 testdata/ 下的 .reg 夹具；夹具是验收基线，缺失即失败。
func loadFixture(t *testing.T, name string) *RegFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("读取夹具失败: %v", err)
	}
	f, err := ParseRegFile(data)
	if err != nil {
		t.Fatalf("解析夹具 %s 失败: %v", name, err)
	}
	return f
}

// TestFixtureHKCUPath 校验 2026-10-13 修复前的用户级 PATH 夹具。
func TestFixtureHKCUPath(t *testing.T) {
	f := loadFixture(t, "HKCU-Environment.reg")
	v, ok := f.PathValue(ScopeUser)
	if !ok {
		t.Fatal("夹具中未找到用户级 Path")
	}
	if v.Kind != KindSZ {
		t.Errorf("夹具用户级 Path 类型: got %s, want REG_SZ", v.Kind)
	}
	if got := len(SplitPathEntries(v.Str)); got != 25 {
		t.Errorf("用户级条目数: got %d, want 25", got)
	}
	if JoinPathEntries(SplitPathEntries(v.Str)) != v.Str {
		t.Error("切分/合并 round trip 失败")
	}
	if !strings.Contains(v.Str, `E:\nvm\.nodejs`) {
		t.Error("夹具应包含 E:\\nvm\\.nodejs（nvm 双工具链事故条目）")
	}
	if pd, err := NewPathData(ScopeUser, v.Str, v.Kind); err != nil || len(pd.Entries) != 25 {
		t.Errorf("由夹具构造 PathData: err=%v, entries=%d", err, len(pd.Entries))
	}
}

// TestFixtureHKLMPath 校验 2026-10-13 修复前的系统级 PATH 夹具。
func TestFixtureHKLMPath(t *testing.T) {
	f := loadFixture(t, "HKLM-Environment.reg")
	v, ok := f.PathValue(ScopeSystem)
	if !ok {
		t.Fatal("夹具中未找到系统级 Path")
	}
	if v.Kind != KindSZ {
		t.Errorf("夹具系统级 Path 类型: got %s, want REG_SZ", v.Kind)
	}
	entries := SplitPathEntries(v.Str)
	if got := len(entries); got != 21 {
		t.Errorf("系统级条目数: got %d, want 21（20 条 + 尾部空条目）", got)
	}
	if entries[len(entries)-1] != "" {
		t.Error("系统级 Path 以 ';' 结尾，最后应切分出空条目")
	}
	if !strings.Contains(v.Str, `F:\微信web开发者工具\dll`) {
		t.Error("夹具应包含中文路径条目（编码回归）")
	}
	if !strings.Contains(v.Str, `F:\NodeJS\node_global`) {
		t.Error("夹具应包含旧 npm 全局目录条目（本次事故根源之一）")
	}
}
