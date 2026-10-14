package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"envwarden/core/mutate"
	"envwarden/core/registry"
)

func TestParseScopes(t *testing.T) {
	tests := []struct {
		in   string
		want []registry.Scope
		ok   bool
	}{
		{"user", []registry.Scope{registry.ScopeUser}, true},
		{"system", []registry.Scope{registry.ScopeSystem}, true},
		{"all", []registry.Scope{registry.ScopeUser, registry.ScopeSystem}, true},
		{"", nil, false},
		{"User", nil, false},
		{"both", nil, false},
	}
	for _, tt := range tests {
		got, ok := parseScopes(tt.in)
		if ok != tt.ok {
			t.Errorf("parseScopes(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			continue
		}
		if ok && strings.Join(scopeStrings(got), ",") != strings.Join(scopeStrings(tt.want), ",") {
			t.Errorf("parseScopes(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// scopeStrings 把作用域列表转为字符串列表便于比较。
func scopeStrings(scopes []registry.Scope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = s.String()
	}
	return out
}

func TestEntryListFlag(t *testing.T) {
	var entries entryList
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.Var(&entries, "entry", "测试")
	if err := fs.Parse([]string{"-entry", `C:\a,b`, "-entry", `F:\中文\bin`}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// -entry 不做逗号拆分：路径中的逗号是合法字符。
	if strings.Join(entries, "|") != `C:\a,b|F:\中文\bin` {
		t.Errorf("entries = %v", []string(entries))
	}

	if err := (&entryList{}).Set(""); err == nil {
		t.Error("空值应返回错误")
	}
}

func TestRenderCleanPlan(t *testing.T) {
	plan := &mutate.RemovalPlan{
		Scope:   registry.ScopeUser,
		NewRaw:  `C:\keep`,
		Kind:    registry.KindSZ,
		Removed: []string{`C:\dead1`, `F:\微信\dll`},
		Kept:    []string{`C:\keep`},
	}
	var buf bytes.Buffer
	renderCleanPlan(&buf, []*mutate.RemovalPlan{plan})
	out := buf.String()
	for _, want := range []string{
		"==== 待移除条目（逐字精确匹配，其余条目逐字保留） ====",
		"  [用户] C:\\dead1",
		"  [用户] F:\\微信\\dll",
		"用户 PATH: 移除 2 条，保留 1 条，值类型 REG_SZ 保持不变",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
}

// TestConfirm 校验确认提示：仅 y / Y 视为确认，其余输入与空输入（非交互）视为取消。
func TestConfirm(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"小写 y", "y\n", true},
		{"大写 Y", "Y\n", true},
		{"带空白", "  y  \n", true},
		{"yes 不是纯 y", "yes\n", false},
		{"n", "n\n", false},
		{"回车默认取消", "\n", false},
		{"空输入（EOF）", "", false},
	}
	for _, tt := range tests {
		if got := confirm(strings.NewReader(tt.input), ""); got != tt.want {
			t.Errorf("%s: confirm(%q) = %v, want %v", tt.name, tt.input, got, tt.want)
		}
	}
}
