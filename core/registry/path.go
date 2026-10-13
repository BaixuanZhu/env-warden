package registry

import (
	"errors"
	"fmt"
	"strings"
)

// valueNamePath 是 PATH 值在注册表中的名字。
const valueNamePath = "Path"

// ValueKind 表示注册表值类型，数值与 Win32 注册表类型编号一致。
type ValueKind uint32

// 注册表值类型常量，仅列本工具会用到的。
const (
	KindNone     ValueKind = 0 // REG_NONE
	KindSZ       ValueKind = 1 // REG_SZ
	KindExpandSZ ValueKind = 2 // REG_EXPAND_SZ
	KindBinary   ValueKind = 3 // REG_BINARY
	KindDWORD    ValueKind = 4 // REG_DWORD
)

// String 返回与注册表工具一致的类型名。
func (k ValueKind) String() string {
	switch k {
	case KindNone:
		return "REG_NONE"
	case KindSZ:
		return "REG_SZ"
	case KindExpandSZ:
		return "REG_EXPAND_SZ"
	case KindBinary:
		return "REG_BINARY"
	case KindDWORD:
		return "REG_DWORD"
	default:
		return fmt.Sprintf("REG_%d", uint32(k))
	}
}

var (
	// ErrPathNotFound 表示作用域键内不存在名为 Path 的值。
	ErrPathNotFound = errors.New("path value not found")
	// ErrUnexpectedKind 表示 Path 值类型不是 REG_SZ / REG_EXPAND_SZ。
	ErrUnexpectedKind = errors.New("unexpected value kind for Path")
)

// PathData 是一个作用域下注册表 Path 原始值的完整快照。
type PathData struct {
	// Scope 值所属的作用域（用户级 / 系统级）。
	Scope Scope
	// Raw 是注册表中的原始字符串，%VAR% 未展开。
	Raw string
	// Kind 是注册表原值类型；写回时必须原样保持，禁止互换。
	Kind ValueKind
	// Entries 是 Raw 按 ';' 切分的条目列表；保留空条目与原始写法，不做归一化。
	Entries []string
}

// NewPathData 由原始字符串与值类型构造 PathData，Entries 由 Raw 派生。
// 仅接受 REG_SZ / REG_EXPAND_SZ，其余类型返回包装 ErrUnexpectedKind 的错误。
func NewPathData(scope Scope, raw string, kind ValueKind) (*PathData, error) {
	if kind != KindSZ && kind != KindExpandSZ {
		return nil, fmt.Errorf("new path data for %s: %w (%s)", scope, ErrUnexpectedKind, kind)
	}
	return &PathData{
		Scope:   scope,
		Raw:     raw,
		Kind:    kind,
		Entries: SplitPathEntries(raw),
	}, nil
}

// SplitPathEntries 按 ';' 切分 PATH 原始字符串。
// 保留空条目（空条目代表"当前目录"，且是逐字写回所必需的信息）；raw 为空时返回 nil。
func SplitPathEntries(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ";")
}

// JoinPathEntries 是 SplitPathEntries 的精确逆操作，保证 Join(Split(raw)) == raw。
func JoinPathEntries(entries []string) string {
	return strings.Join(entries, ";")
}
