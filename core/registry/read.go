package registry

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// rootAndSub 返回作用域对应的注册表根键与子键路径。
func (s Scope) rootAndSub() (root registry.Key, sub string) {
	if s == ScopeUser {
		return registry.CURRENT_USER, `Environment`
	}
	return registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
}

// ReadPath 从注册表读取指定作用域的 Path 原始值（"新终端视角"，%VAR% 未展开）。
//
// 绝不读取当前进程的 PATH 环境变量——已开着的终端持有旧值。
// registry.GetStringValue 基于原始 RegQueryValueEx 数据解码，不会展开 REG_EXPAND_SZ；
// 返回的 PathData.Kind 反映注册表原值类型，写回时必须原样保持。
func ReadPath(scope Scope) (*PathData, error) {
	root, sub := scope.rootAndSub()
	k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("read path: open %s: %w", scope.KeyPath(), err)
	}
	defer k.Close()

	val, typ, err := k.GetStringValue(valueNamePath)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, fmt.Errorf("read path: %s: %w", scope.KeyPath(), ErrPathNotFound)
		}
		if errors.Is(err, registry.ErrUnexpectedType) {
			return nil, fmt.Errorf("read path: %s: %w (%d)", scope.KeyPath(), ErrUnexpectedKind, typ)
		}
		return nil, fmt.Errorf("read path: %s: %w", scope.KeyPath(), err)
	}
	return NewPathData(scope, val, ValueKind(typ))
}
