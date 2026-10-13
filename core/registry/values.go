package registry

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// ReadStringValues 读取作用域键下全部字符串类环境变量（REG_SZ / REG_EXPAND_SZ），
// 返回 值名大写 → 原始字符串（REG_EXPAND_SZ 不展开 %VAR%）。
// 非字符串值（DWORD 等）跳过。
func ReadStringValues(scope Scope) (map[string]string, error) {
	root, sub := scope.rootAndSub()
	k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("read values: open %s: %w", scope.KeyPath(), err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("read values: %s: %w", scope.KeyPath(), err)
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) || errors.Is(err, registry.ErrUnexpectedType) {
				continue
			}
			return nil, fmt.Errorf("read values: %s\\%s: %w", scope.KeyPath(), name, err)
		}
		out[strings.ToUpper(name)] = v
	}
	return out, nil
}
