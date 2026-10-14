package ops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"envwarden/core/backup"
	"envwarden/core/broadcast"
	"envwarden/core/registry"
)

// ErrUnknownScopeKey 表示 .reg 文件中的键路径不对应任何已知作用域。
var ErrUnknownScopeKey = errors.New("reg file contains no known Environment key")

// RestoreResult 是一次还原的结果；即使伴随错误返回也可能携带已写出的备份路径。
type RestoreResult struct {
	// Scope 还原的作用域。
	Scope registry.Scope
	// Backups 还原前对当前状态强制产出的备份路径。
	Backups []string
	// Written 实际写回的值名（按备份文件中的顺序）。
	Written []string
	// BroadcastErr WM_SETTINGCHANGE 广播错误；变更已生效，仅作为警告上报。
	BroadcastErr error
}

// RestoreFile 将 .reg 备份文件整键写回注册表（按文件内容自动识别作用域）。
//
// 安全顺序：解析并识别作用域 → 系统级提权预检 → 强制备份当前值 → 逐值类型保持写入
// → 写后校验 → 广播。与 regedit 导入语义一致：只写文件中出现的值，不删除键内其他值；
// 值类型以备份文件为准（还原即回到备份时刻的状态，包括当时的 Path 值类型）。
// 接受本工具生成的备份，也接受 regedit 导出的同键 .reg 文件。
func RestoreFile(backupDir, path string, now time.Time) (*RestoreResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("restore: read %s: %w", path, err)
	}
	f, err := registry.ParseRegFile(data)
	if err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}
	scope, values, err := ScopeFromRegFile(f)
	if err != nil {
		return nil, fmt.Errorf("restore %s: %w", path, err)
	}
	res := &RestoreResult{Scope: scope}

	if scope == registry.ScopeSystem && !registry.IsElevated() {
		return res, fmt.Errorf("restore: %s: %w: run from an elevated terminal",
			scope, registry.ErrAccessDenied)
	}

	if backupPath, err := backup.WriteScopeFile(backupDir, scope, now); err != nil {
		return res, fmt.Errorf("restore: backup current %s: %w", scope, err)
	} else {
		res.Backups = append(res.Backups, backupPath)
	}

	if err := registry.WriteValues(scope, values); err != nil {
		return res, fmt.Errorf("restore: write %s: %w", scope, err)
	}

	current, err := registry.ReadAllValues(scope)
	if err != nil {
		return res, fmt.Errorf("restore: verify read %s: %w", scope, err)
	}
	present := make(map[string]registry.RegValue, len(current))
	for _, v := range current {
		present[lowerName(v.Name)] = v
	}
	for _, v := range values {
		got, ok := present[lowerName(v.Name)]
		if !ok || !ValueMatches(v, got) {
			return res, fmt.Errorf("restore: verify %s\\%s: %w", scope.KeyPath(), v.Name, ErrVerifyFailed)
		}
		res.Written = append(res.Written, v.Name)
	}

	res.BroadcastErr = broadcast.NotifyEnvironmentChange()
	return res, nil
}

// ScopeFromRegFile 从解析后的 .reg 内容中找出唯一一个已知作用域键及其值列表。
// 含多个或零个已知键均返回错误（本工具的备份总是单键导出）。
func ScopeFromRegFile(f *registry.RegFile) (registry.Scope, []registry.RegValue, error) {
	var scopes []registry.Scope
	var matched registry.Scope
	var values []registry.RegValue
	for _, scope := range []registry.Scope{registry.ScopeUser, registry.ScopeSystem} {
		if vs, ok := f.Keys[strings.ToLower(scope.KeyPath())]; ok {
			scopes = append(scopes, scope)
			matched, values = scope, vs
		}
	}
	switch len(scopes) {
	case 1:
		return matched, values, nil
	case 0:
		return 0, nil, fmt.Errorf("%w: want one of [HKEY_CURRENT_USER\\Environment | HKEY_LOCAL_MACHINE\\...\\Environment]",
			ErrUnknownScopeKey)
	default:
		return 0, nil, fmt.Errorf("%w: multiple scopes in one file", ErrUnknownScopeKey)
	}
}

// lowerName 转小写值名；注册表值名不区分大小写。
func lowerName(s string) string {
	return strings.ToLower(s)
}

// ValueMatches 判断注册表中现存值 got 是否与备份值 want 完全一致（类型与数据）。
// 字符串类比较解码后的 Str，DWORD 比较 Dword，其余类型比较原始字节。
func ValueMatches(want, got registry.RegValue) bool {
	if want.Kind != got.Kind {
		return false
	}
	switch want.Kind {
	case registry.KindSZ, registry.KindExpandSZ:
		return want.Str == got.Str
	case registry.KindDWORD:
		return want.Dword == got.Dword
	default:
		return bytes.Equal(want.Raw, got.Raw)
	}
}
