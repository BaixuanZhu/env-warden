package detect

import (
	"regexp"
	"strings"
)

// Vars 是展开 %VAR% 用的环境变量表，键统一大写（来源：两级注册表合成）。
type Vars map[string]string

// envVarPattern 匹配 %NAME% 形式的环境变量引用，变量名大小写不敏感。
var envVarPattern = regexp.MustCompile(`(?i)%([A-Z0-9_]+)%`)

// maxExpandDepth 限制变量值递归展开深度，防止循环引用（A=%B%、B=%A%）死循环。
const maxExpandDepth = 5

// ExpandEnv 用 vars 展开 s 中的 %VAR%；未定义的变量原样保留。
// 变量值自身含 %VAR% 引用时递归展开。
func ExpandEnv(s string, vars Vars) string {
	return expandString(s, vars, 0)
}

func expandString(s string, vars Vars, depth int) string {
	if depth > maxExpandDepth {
		return s
	}
	return envVarPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.ToUpper(m[1 : len(m)-1])
		if v, ok := vars[name]; ok {
			return expandString(v, vars, depth+1)
		}
		return m
	})
}

// ComposeVars 合并两级环境变量表（用户级覆盖系统级），键统一大写。
// 变量值中的 %VAR% 引用（如 TMP=%USERPROFILE%\AppData\Local\Temp）做定点展开。
func ComposeVars(system, user map[string]string) Vars {
	merged := make(Vars, len(system)+len(user))
	for k, v := range system {
		merged[strings.ToUpper(k)] = v
	}
	for k, v := range user {
		merged[strings.ToUpper(k)] = v
	}
	for round := 0; round < maxExpandDepth; round++ {
		changed := false
		for k, v := range merged {
			if nv := expandString(v, merged, 1); nv != v {
				merged[k] = nv
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return merged
}
