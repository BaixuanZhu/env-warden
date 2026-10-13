package detect

import "testing"

func TestExpandEnv(t *testing.T) {
	vars := Vars{"SYSTEMROOT": `C:\Windows`}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"展开已定义变量（大小写不敏感）", `%SystemRoot%\system32`, `C:\Windows\system32`},
		{"未定义变量原样保留", `%FOO%\bar`, `%FOO%\bar`},
		{"无变量原样返回", `C:\plain`, `C:\plain`},
		{"变量值引用其他变量时递归展开", `%A%`, `C:\y\x`},
	}
	vars2 := Vars{"A": `%B%\x`, "B": `C:\y`}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := vars
			if tt.name == "变量值引用其他变量时递归展开" {
				v = vars2
			}
			if got := ExpandEnv(tt.in, v); got != tt.want {
				t.Errorf("ExpandEnv(%q): got %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandEnvCycleTerminates(t *testing.T) {
	vars := Vars{"A": `%B%`, "B": `%A%`}
	// 循环引用不应死循环；结果内容不限，只验证能返回。
	_ = ExpandEnv(`%A%`, vars)
}

func TestComposeVars(t *testing.T) {
	system := map[string]string{
		"PATH":        "x",
		"USERPROFILE": `C:\Users\default`,
		"TEMP":        `%USERPROFILE%\Local\Temp`,
	}
	user := map[string]string{
		"USERPROFILE": `C:\Users\actual`,
	}
	merged := ComposeVars(system, user)
	if got := merged["USERPROFILE"]; got != `C:\Users\actual` {
		t.Errorf("用户级应覆盖系统级: got %q", got)
	}
	if got := merged["TEMP"]; got != `C:\Users\actual\Local\Temp` {
		t.Errorf("变量值中的 %%USERPROFILE%% 应按覆盖后的值展开: got %q", got)
	}
}
