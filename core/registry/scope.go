package registry

// Scope 表示 Windows 注册表两级 Environment 键的作用域。
//
// Windows 命令解析时系统级 PATH 拼在用户级之前，同名命令系统级胜出，
// 因此两级必须分别读取、分别报告。
type Scope int

// 两级作用域。
const (
	// ScopeUser 用户级：HKCU\Environment。
	ScopeUser Scope = iota
	// ScopeSystem 系统级：HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment。
	ScopeSystem
)

// String 返回小写作用域名，用于报告与 CLI 输出。
func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeSystem:
		return "system"
	default:
		return "unknown"
	}
}

// KeyPath 返回与 regedit 导出格式一致的完整键路径。
func (s Scope) KeyPath() string {
	switch s {
	case ScopeUser:
		return `HKEY_CURRENT_USER\Environment`
	case ScopeSystem:
		return `HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	default:
		return ""
	}
}
