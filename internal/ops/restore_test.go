package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"envwarden/core/registry"
)

func TestScopeFromRegFile(t *testing.T) {
	// ParseRegFile 的键索引是小写键路径，构造输入遵循同一口径。
	ul := `hkey_current_user\environment`
	sl := `hkey_local_machine\system\currentcontrolset\control\session manager\environment`
	tests := []struct {
		name     string
		keyPaths []string
		want     registry.Scope
		wantErr  error
	}{
		{name: "用户级键", keyPaths: []string{ul}, want: registry.ScopeUser},
		{name: "系统级键", keyPaths: []string{sl}, want: registry.ScopeSystem},
		{name: "无已知键", keyPaths: []string{`hkey_current_user\software\other`}, wantErr: ErrUnknownScopeKey},
		{name: "两个作用域并存", keyPaths: []string{ul, sl}, wantErr: ErrUnknownScopeKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &registry.RegFile{Keys: map[string][]registry.RegValue{}}
			for _, k := range tt.keyPaths {
				f.Keys[k] = append(f.Keys[k], registry.RegValue{Name: "Path", Kind: registry.KindSZ, Str: "x"})
			}
			scope, values, err := ScopeFromRegFile(f)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ScopeFromRegFile err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ScopeFromRegFile: %v", err)
			}
			if scope != tt.want {
				t.Errorf("Scope = %s, want %s", scope, tt.want)
			}
			if len(values) != 1 || values[0].Name != "Path" {
				t.Errorf("values = %+v, want 键下的值列表", values)
			}
		})
	}
}

func TestValueMatches(t *testing.T) {
	tests := []struct {
		name string
		want registry.RegValue
		got  registry.RegValue
		ok   bool
	}{
		{
			name: "REG_SZ 一致",
			want: registry.RegValue{Name: "A", Kind: registry.KindSZ, Str: `C:\x`},
			got:  registry.RegValue{Name: "a", Kind: registry.KindSZ, Str: `C:\x`},
			ok:   true,
		},
		{
			name: "REG_SZ 与 REG_EXPAND_SZ 类型不同",
			want: registry.RegValue{Kind: registry.KindSZ, Str: "x"},
			got:  registry.RegValue{Kind: registry.KindExpandSZ, Str: "x"},
			ok:   false,
		},
		{
			name: "EXPAND_SZ 数据不同",
			want: registry.RegValue{Kind: registry.KindExpandSZ, Str: `%A%\bin`},
			got:  registry.RegValue{Kind: registry.KindExpandSZ, Str: `%B%\bin`},
			ok:   false,
		},
		{
			name: "DWORD 一致",
			want: registry.RegValue{Kind: registry.KindDWORD, Dword: 2},
			got:  registry.RegValue{Kind: registry.KindDWORD, Dword: 2},
			ok:   true,
		},
		{
			name: "BINARY 字节不同",
			want: registry.RegValue{Kind: registry.KindBinary, Raw: []byte{1, 2}},
			got:  registry.RegValue{Kind: registry.KindBinary, Raw: []byte{1, 3}},
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValueMatches(tt.want, tt.got); got != tt.ok {
				t.Errorf("ValueMatches = %v, want %v", got, tt.ok)
			}
		})
	}
}

// TestRestoreScopeFromFixture 用真实修复前夹具验证：文件 → 解析 → 识别作用域链路可用。
func TestRestoreScopeFromFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "HKCU-Environment.reg"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	f, err := registry.ParseRegFile(data)
	if err != nil {
		t.Fatalf("ParseRegFile: %v", err)
	}
	scope, _, err := ScopeFromRegFile(f)
	if err != nil {
		t.Fatalf("ScopeFromRegFile: %v", err)
	}
	if scope != registry.ScopeUser {
		t.Errorf("Scope = %s, want user", scope)
	}
}
