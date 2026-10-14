package registry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ErrAccessDenied 表示因权限不足被系统拒绝，典型场景是未提权写 HKLM。
var ErrAccessDenied = errors.New("registry access denied")

// WritePath 将 PathData 原样写回注册表，严格保持原值类型：
// REG_SZ 与 REG_EXPAND_SZ 各走对应的 SetValue 变体，禁止互换，
// 否则 %VAR% 会在 REG_SZ 下不再展开且无任何报错（静默损坏）。
// HKLM 写入需要管理员权限，未提权时返回包装 ErrAccessDenied 的错误，绝不静默降级。
func WritePath(pd *PathData) error {
	if pd.Kind != KindSZ && pd.Kind != KindExpandSZ {
		return fmt.Errorf("write path %s: %w (%s)", pd.Scope.KeyPath(), ErrUnexpectedKind, pd.Kind)
	}
	root, sub := pd.Scope.rootAndSub()
	k, err := registry.OpenKey(root, sub, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("write path: open %s: %w", pd.Scope.KeyPath(), mapAccessErr(err))
	}
	defer k.Close()

	var setErr error
	if pd.Kind == KindExpandSZ {
		setErr = k.SetExpandStringValue(valueNamePath, pd.Raw)
	} else {
		setErr = k.SetStringValue(valueNamePath, pd.Raw)
	}
	if setErr != nil {
		return fmt.Errorf("write path: set %s\\Path: %w", pd.Scope.KeyPath(), mapAccessErr(setErr))
	}
	return nil
}

// WriteValues 将一组值写回作用域键，逐值保持类型（REG_SZ / REG_EXPAND_SZ /
// REG_DWORD / REG_QWORD / REG_MULTI_SZ / REG_BINARY 各走对应的 SetValue 变体）。
// 用于从备份整键还原；只写传入的值，不删除键内其他值（与 regedit 导入语义一致）。
// HKLM 写入需要管理员权限，未提权时返回包装 ErrAccessDenied 的错误。
func WriteValues(scope Scope, values []RegValue) error {
	root, sub := scope.rootAndSub()
	k, err := registry.OpenKey(root, sub, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("write values: open %s: %w", scope.KeyPath(), mapAccessErr(err))
	}
	defer k.Close()
	for _, v := range values {
		if err := setValue(k, v); err != nil {
			return fmt.Errorf("write values: set %s\\%s: %w", scope.KeyPath(), v.Name, mapAccessErr(err))
		}
	}
	return nil
}

// setValue 按值类型选择对应的 SetValue 变体，保证还原后类型逐值不变。
func setValue(k registry.Key, v RegValue) error {
	switch v.Kind {
	case KindSZ:
		return k.SetStringValue(v.Name, v.Str)
	case KindExpandSZ:
		return k.SetExpandStringValue(v.Name, v.Str)
	case KindDWORD:
		return k.SetDWordValue(v.Name, v.Dword)
	case KindQWord:
		if len(v.Raw) != 8 {
			return fmt.Errorf("qword %s: expect 8 bytes, got %d", v.Name, len(v.Raw))
		}
		return k.SetQWordValue(v.Name, binary.LittleEndian.Uint64(v.Raw))
	case KindMultiSZ:
		return k.SetStringsValue(v.Name, decodeMultiSZ(v.Raw))
	default:
		// REG_BINARY、REG_NONE 及未知类型按原始字节写回，不丢信息。
		return k.SetBinaryValue(v.Name, v.Raw)
	}
}

// decodeMultiSZ 将 REG_MULTI_SZ 原始字节（UTF-16LE，字符串以 NUL 分隔、双 NUL 结尾）
// 解码为字符串列表；结尾的空串（双 NUL 产生）丢弃。
func decodeMultiSZ(b []byte) []string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	var out []string
	cur := strings.Builder{}
	for i := 0; i < len(b); i += 2 {
		c := rune(uint16(b[i]) | uint16(b[i+1])<<8)
		if c == 0 {
			if cur.Len() == 0 {
				continue // 结尾双 NUL 的第二次 NUL
			}
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(c)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ReadAllValues 读取作用域 Environment 键下全部值（含类型与原始字节），供备份导出。
// 返回顺序与注册表枚举顺序一致；REG_EXPAND_SZ 的 %VAR% 不展开。
// 字符串类值解码进 Str，REG_DWORD 另存 Dword，其余类型保留 Raw。
func ReadAllValues(scope Scope) ([]RegValue, error) {
	root, sub := scope.rootAndSub()
	k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("read all values: open %s: %w", scope.KeyPath(), err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("read all values: %s: %w", scope.KeyPath(), err)
	}
	out := make([]RegValue, 0, len(names))
	for _, name := range names {
		n, _, err := k.GetValue(name, nil)
		if err != nil && !errors.Is(err, registry.ErrShortBuffer) {
			return nil, fmt.Errorf("read all values: %s\\%s: %w", scope.KeyPath(), name, err)
		}
		buf := make([]byte, n)
		n, typ, err := k.GetValue(name, buf)
		if err != nil {
			return nil, fmt.Errorf("read all values: %s\\%s: %w", scope.KeyPath(), name, err)
		}
		out = append(out, decodeRawValue(name, ValueKind(typ), buf[:n]))
	}
	return out, nil
}

// IsElevated 报告当前进程是否持有提升（管理员）令牌，用于 HKLM 写入前的提权预检。
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// decodeRawValue 将 RegQueryValueEx 的原始字节按类型解码为 RegValue。
func decodeRawValue(name string, kind ValueKind, data []byte) RegValue {
	v := RegValue{Name: name, Kind: kind, Raw: append([]byte(nil), data...)}
	switch kind {
	case KindSZ, KindExpandSZ:
		v.Str = strings.TrimRight(utf16LEToString(data), "\x00")
	case KindDWORD:
		if len(data) == 4 {
			v.Dword = binary.LittleEndian.Uint32(data)
		}
	}
	return v
}

// utf16LEToString 将注册表返回的 UTF-16LE 原始字节解码为字符串。
func utf16LEToString(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return windows.UTF16ToString(u)
}

// mapAccessErr 将 ERROR_ACCESS_DENIED 归一为 ErrAccessDenied 哨兵，
// 便于 CLI 定向给出"需管理员终端"提示而非笼统报错。
func mapAccessErr(err error) error {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w: %w", ErrAccessDenied, err)
	}
	return err
}
