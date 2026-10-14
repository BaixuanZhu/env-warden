package backup

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"envwarden/core/registry"
)

func TestQuoteReg(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"普通路径", `C:\Program Files`, `"C:\\Program Files"`},
		{"含引号", `he said "hi"`, `"he said \"hi\""`},
		{"中文路径", `F:\微信web开发者工具\dll`, `"F:\\微信web开发者工具\\dll"`},
		{"空串", ``, `""`},
	}
	for _, tt := range tests {
		if got := quoteReg(tt.in); got != tt.want {
			t.Errorf("%s: quoteReg(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// TestWrapHexLineGolden 用真实 regedit 导出的 JAVA_HOME 值做逐字节黄金对照，
// 锁定续行规则（首行 80 列、续行 2 空格缩进 78 列、行尾 '\'）。
func TestWrapHexLineGolden(t *testing.T) {
	var b strings.Builder
	writeValue(&b, registry.RegValue{
		Name: "JAVA_HOME",
		Kind: registry.KindExpandSZ,
		Str:  `C:\Users\zbxComputer\.jvm\current`,
	})
	want := []string{
		`"JAVA_HOME"=hex(2):43,00,3a,00,5c,00,55,00,73,00,65,00,72,00,73,00,5c,00,7a,00,\`,
		`  62,00,78,00,43,00,6f,00,6d,00,70,00,75,00,74,00,65,00,72,00,5c,00,2e,00,6a,\`,
		`  00,76,00,6d,00,5c,00,63,00,75,00,72,00,72,00,65,00,6e,00,74,00,00,00`,
	}
	got := strings.Split(strings.TrimSuffix(b.String(), "\r\n"), "\r\n")
	if len(got) != len(want) {
		t.Fatalf("续行行数 = %d, want %d:\n%s", len(got), len(want), b.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行:\n got  %q (%d 列)\n want %q (%d 列)",
				i+1, got[i], len(got[i]), want[i], len(want[i]))
		}
	}
}

// TestFormatRegFileStructure 校验输出字节流的结构特征：BOM、CRLF、键行、空行收尾。
func TestFormatRegFileStructure(t *testing.T) {
	out := FormatRegFile(registry.ScopeUser, []registry.RegValue{
		{Name: "Greeting", Kind: registry.KindSZ, Str: "你好"},
	})
	if !bytes.HasPrefix(out, []byte{0xFF, 0xFE}) {
		t.Fatalf("缺少 UTF-16LE BOM: % x", out[:4])
	}
	// 尾部为值行结尾的 CRLF 加一个空行（CRLF），与 regedit 导出一致。
	if !bytes.HasSuffix(out, utf16le("\r\n\r\n")) {
		t.Errorf("未以空行收尾: % x", out[len(out)-8:])
	}
	if !bytes.Contains(out, utf16le("[HKEY_CURRENT_USER\\Environment]\r\n")) {
		t.Errorf("缺少键行")
	}
	if !bytes.Contains(out, utf16le(`"Greeting"="你好"`)) {
		t.Errorf("缺少 REG_SZ 值行")
	}
}

// TestFormatValueKinds 覆盖各值类型的序列化与回读语义。
func TestFormatValueKinds(t *testing.T) {
	values := []registry.RegValue{
		{Name: "SZVal", Kind: registry.KindSZ, Str: `E:\nvm\.nodejs`},
		{Name: "ExpandVal", Kind: registry.KindExpandSZ, Str: `%SystemRoot%\system32`},
		{Name: "DwordVal", Kind: registry.KindDWORD, Dword: 0x000000ff},
		{Name: "BinaryVal", Kind: registry.KindBinary, Raw: []byte{0xde, 0xad, 0xbe}},
		{Name: "MultiVal", Kind: registry.KindMultiSZ, Raw: utf16le("a\x00b\x00")},
		{Name: "QwordVal", Kind: registry.KindQWord, Raw: []byte{1, 0, 0, 0, 0, 0, 0, 0}},
	}
	out := FormatRegFile(registry.ScopeSystem, values)
	f, err := registry.ParseRegFile(out)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	for i, want := range values {
		got, ok := f.Value(registry.ScopeSystem.KeyPath(), want.Name)
		if !ok {
			t.Fatalf("值 %s 回读缺失", want.Name)
		}
		if got.Kind != want.Kind {
			t.Errorf("%s 类型回读 = %v, want %v", want.Name, got.Kind, want.Kind)
		}
		switch want.Kind {
		case registry.KindSZ, registry.KindExpandSZ:
			if got.Str != want.Str {
				t.Errorf("%s 字符串回读 = %q, want %q", want.Name, got.Str, want.Str)
			}
		case registry.KindDWORD:
			if got.Dword != want.Dword {
				t.Errorf("%s dword 回读 = %#x, want %#x", want.Name, got.Dword, want.Dword)
			}
		default:
			if !bytes.Equal(got.Raw, want.Raw) {
				t.Errorf("%s 原始字节回读 = % x, want % x", want.Name, got.Raw, want.Raw)
			}
		}
		_ = i
	}
}

// TestRoundTripFixture 用真实 regedit 导出夹具做序列化回环：
// 序列化 → 再解析 → 语义逐值相等，保证备份还原不丢信息。
func TestRoundTripFixture(t *testing.T) {
	tests := []struct {
		scope registry.Scope
		file  string
	}{
		{registry.ScopeUser, "../../testdata/HKCU-Environment.reg"},
		{registry.ScopeSystem, "../../testdata/HKLM-Environment.reg"},
	}
	for _, tt := range tests {
		t.Run(tt.scope.String(), func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("读取夹具: %v", err)
			}
			orig, err := registry.ParseRegFile(data)
			if err != nil {
				t.Fatalf("解析夹具: %v", err)
			}
			values := orig.Keys[strings.ToLower(tt.scope.KeyPath())]
			if len(values) == 0 {
				t.Fatalf("夹具 %s 无值", tt.scope.KeyPath())
			}
			again, err := registry.ParseRegFile(FormatRegFile(tt.scope, values))
			if err != nil {
				t.Fatalf("回读序列化结果: %v", err)
			}
			got := again.Keys[strings.ToLower(tt.scope.KeyPath())]
			if len(got) != len(values) {
				t.Fatalf("回读值数 = %d, want %d", len(got), len(values))
			}
			for i, want := range values {
				g := got[i]
				if g.Name != want.Name || g.Kind != want.Kind {
					t.Fatalf("第 %d 个值: got (%s,%v), want (%s,%v)", i, g.Name, g.Kind, want.Name, want.Kind)
				}
				if g.Str != want.Str || g.Dword != want.Dword {
					t.Fatalf("值 %s 数据不一致: got (str=%q,dword=%#x), want (str=%q,dword=%#x)",
						want.Name, g.Str, g.Dword, want.Str, want.Dword)
				}
				switch want.Kind {
				case registry.KindSZ, registry.KindExpandSZ, registry.KindDWORD:
					// 字符串与 dword 的 Raw 允许 NUL 终止符差异，语义相等即可。
				default:
					if !bytes.Equal(g.Raw, want.Raw) {
						t.Fatalf("值 %s 原始字节不一致", want.Name)
					}
				}
			}
		})
	}
}

func TestFileName(t *testing.T) {
	now := time.Date(2026, 10, 14, 15, 30, 1, 0, time.Local)
	if got := FileName(registry.ScopeUser, now); got != "envwarden-backup-user-20261014-153001.reg" {
		t.Errorf("FileName = %q", got)
	}
	if got := FileName(registry.ScopeSystem, now); got != "envwarden-backup-system-20261014-153001.reg" {
		t.Errorf("FileName = %q", got)
	}
}

// utf16le 是测试用的 UTF-16LE 编码帮助函数。
func utf16le(s string) []byte {
	return encodeUTF16LE(s)[2:] // 去掉 BOM
}
