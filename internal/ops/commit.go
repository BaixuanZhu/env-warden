// Package ops 提供 CLI 与 GUI 共用的"已确认变更"提交流水线。
//
// 安全顺序固定且不可跳过：变更前复检 → 强制备份 → 类型保持写入 → 写后校验 → 广播。
// 壳（CLI / GUI）负责计划展示与用户确认；本包只做提交，且任何失败都不静默：
// 写入前的失败不触碰注册表，写入后的失败通过返回的 CommitResult 指向已产出的备份。
package ops

import (
	"errors"
	"fmt"
	"time"

	"envwarden/core/backup"
	"envwarden/core/broadcast"
	"envwarden/core/registry"
)

// ErrPathChanged 表示生成计划到提交之间 PATH 被外部修改，本次变更必须放弃并重新体检。
var ErrPathChanged = errors.New("PATH changed externally since plan was made")

// ErrVerifyFailed 表示写入后回读结果与计划不一致；此时注册表状态可疑，应指向备份还原。
var ErrVerifyFailed = errors.New("post-write verification failed")

// PathChange 是一笔已确认的 PATH 变更；OldRaw 是生成计划时刻的原文，仅用于变更前复检。
type PathChange struct {
	// Scope 目标作用域。
	Scope registry.Scope
	// OldRaw 计划时刻的 PATH 原文。
	OldRaw string
	// NewRaw 待写入的新 PATH 原文（条目必须与 OldRaw 构成声明的精确变更）。
	NewRaw string
	// Kind 值类型（REG_SZ / REG_EXPAND_SZ），写回时必须原样保持。
	Kind registry.ValueKind
}

// CommitResult 是一次提交的结果；即使伴随错误返回也可能携带已写出的备份路径。
type CommitResult struct {
	// Backups 本次提交强制产出的 .reg 备份路径（还原 = 双击导入）。
	Backups []string
	// BroadcastErr WM_SETTINGCHANGE 广播错误；变更已生效，仅作为警告上报，不视为提交失败。
	BroadcastErr error
}

// CommitPath 按固定安全顺序提交一笔或多笔 PATH 变更（每个作用域至多一笔）。
// 任一步失败立即中止；系统级写入需要调用方先行提权预检，未提权时本包也会
// 因 ErrAccessDenied 失败而不会静默降级。
func CommitPath(backupDir string, changes []PathChange, now time.Time) (*CommitResult, error) {
	if len(changes) == 0 {
		return nil, errors.New("commit path: no changes declared")
	}
	res := &CommitResult{}

	// 1. 变更前复检：确认期间 PATH 被外部修改则放弃，避免基于旧快照覆盖新变更。
	for _, c := range changes {
		cur, err := registry.ReadPath(c.Scope)
		if err != nil {
			return res, fmt.Errorf("commit path: re-read %s: %w", c.Scope, err)
		}
		if cur.Raw != c.OldRaw {
			return res, fmt.Errorf("commit path: %s: %w", c.Scope, ErrPathChanged)
		}
	}

	// 2. 强制备份（不可跳过）：仅备份将被修改的作用域，失败即中止且未触碰注册表。
	for _, c := range changes {
		path, err := backup.WriteScopeFile(backupDir, c.Scope, now)
		if err != nil {
			return res, fmt.Errorf("commit path: backup %s: %w", c.Scope, err)
		}
		res.Backups = append(res.Backups, path)
	}

	// 3. 写入并逐笔校验：条目与值类型必须与计划一致。
	for _, c := range changes {
		pd, err := registry.NewPathData(c.Scope, c.NewRaw, c.Kind)
		if err != nil {
			return res, fmt.Errorf("commit path: %w", err)
		}
		if err := registry.WritePath(pd); err != nil {
			return res, fmt.Errorf("commit path: write %s: %w", c.Scope, err)
		}
		cur, err := registry.ReadPath(c.Scope)
		if err != nil || cur.Raw != c.NewRaw || cur.Kind != c.Kind {
			return res, fmt.Errorf("commit path: verify %s: %w: %v", c.Scope, ErrVerifyFailed, err)
		}
	}

	// 4. 广播：失败不回滚（变更已生效），仅警告。
	res.BroadcastErr = broadcast.NotifyEnvironmentChange()
	return res, nil
}
