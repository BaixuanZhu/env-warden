package registry

import (
	"testing"
	"unicode/utf16"

	winreg "golang.org/x/sys/windows/registry"
)

// encodeMultiSZForTest 编码 MULTI_SZ 测试输入：UTF-16LE + 每串 NUL 结尾 + 双 NUL 收尾。
func encodeMultiSZForTest(ss []string) []byte {
	var u []uint16
	for _, s := range ss {
		u = append(u, utf16.Encode([]rune(s))...)
		u = append(u, 0)
	}
	u = append(u, 0)
	out := make([]byte, 0, 2*len(u))
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

func TestDecodeMultiSZ(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want []string
	}{
		{name: "常规多串", raw: encodeMultiSZForTest([]string{`C:\a`, `C:\b`}), want: []string{`C:\a`, `C:\b`}},
		{name: "单串", raw: encodeMultiSZForTest([]string{`C:\a`}), want: []string{`C:\a`}},
		{name: "空列表", raw: encodeMultiSZForTest(nil), want: nil},
		{name: "中文字符串", raw: encodeMultiSZForTest([]string{`F:\微信`}), want: []string{`F:\微信`}},
		{name: "奇数长度丢弃残字节", raw: []byte{'a', 0, 0}, want: []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeMultiSZ(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("decodeMultiSZ = %q, want %q", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("decodeMultiSZ[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestSetValueKindCoverage 对全部类型走一遍 setValue 分支选择（用无效句柄触发，
// 只要求不 panic、返回错误即可）；真实写入的正确性由冒烟测试覆盖。
func TestSetValueKindCoverage(t *testing.T) {
	var k winreg.Key // 零值句柄，任何 SetValue 都会失败
	for _, tt := range []struct {
		kind ValueKind
		v    RegValue
	}{
		{KindSZ, RegValue{Name: "x", Kind: KindSZ, Str: "s"}},
		{KindExpandSZ, RegValue{Name: "x", Kind: KindExpandSZ, Str: "s"}},
		{KindDWORD, RegValue{Name: "x", Kind: KindDWORD, Dword: 1}},
		{KindQWord, RegValue{Name: "x", Kind: KindQWord, Raw: make([]byte, 8)}},
		{KindQWord, RegValue{Name: "x", Kind: KindQWord, Raw: make([]byte, 3)}}, // 长度异常分支
		{KindMultiSZ, RegValue{Name: "x", Kind: KindMultiSZ, Raw: encodeMultiSZForTest([]string{"a"})}},
		{KindBinary, RegValue{Name: "x", Kind: KindBinary, Raw: []byte{0}}},
	} {
		if err := setValue(k, tt.v); err == nil {
			t.Errorf("setValue(%s) on invalid handle: expect error", tt.kind)
		}
	}
}
