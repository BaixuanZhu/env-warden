// Package gui 是 env-warden 的 Wails 桌面壳：只做参数组装、结果转换与错误翻译，
// 全部业务逻辑在 core 与 internal/ops，与 CLI 壳等价、可互相替换。
package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"envwarden/core/backup"
	"envwarden/core/detect"
	"envwarden/core/mutate"
	"envwarden/core/registry"
	"envwarden/core/snapshot"
	"envwarden/internal/cli"
	"envwarden/internal/ops"
)

// Version 是 GUI 展示的当前构建版本，与 CLI -v 共用同一来源。
const Version = "0.0.1-dev"

// App 是绑定给前端的 GUI 服务层；所有方法返回 JSON 可序列化结果，
// 错误一律翻译为面向用户的中文提示（含备份路径）。
type App struct {
	ctx         context.Context
	backupDir   string
	snapshotDir string
}

// NewApp 构造 App，备份与快照目录固定在 %LOCALAPPDATA%\envwarden 下，
// 与 CLI 的默认快照目录一致（GUI 的体检快照可供 diff 子命令使用）。
func NewApp() *App {
	base := os.Getenv("LOCALAPPDATA")
	var backupDir, snapshotDir string
	if base == "" {
		backupDir, snapshotDir = backup.BackupDirName, snapshot.DirName
	} else {
		backupDir = filepath.Join(base, "envwarden", "backups")
		snapshotDir = filepath.Join(base, "envwarden", "snapshots")
	}
	return &App{backupDir: backupDir, snapshotDir: snapshotDir}
}

// startup 保存 Wails 运行时上下文（以管理员重启退出当前窗口时使用）。
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// HealthInfo 是 GUI 启动信息。
type HealthInfo struct {
	// Version 构建版本。
	Version string `json:"version"`
	// Elevated 当前进程是否持管理员令牌（系统级写入的前提）。
	Elevated bool `json:"elevated"`
	// BackupDir 自动备份目录。
	BackupDir string `json:"backupDir"`
	// SnapshotDir 快照目录。
	SnapshotDir string `json:"snapshotDir"`
}

// Health 返回启动信息（版本、是否已提权、数据目录）。
func (a *App) Health() HealthInfo {
	return HealthInfo{
		Version:     Version,
		Elevated:    registry.IsElevated(),
		BackupDir:   a.backupDir,
		SnapshotDir: a.snapshotDir,
	}
}

// EntryView 是单个 PATH 条目的前端视图。
type EntryView struct {
	// Position 原始切分位置（1 起，与 diff 报告口径一致）。
	Position int `json:"position"`
	// Entry 条目原文（逐字写回的依据）。
	Entry string `json:"entry"`
	// Expanded %VAR% 展开后的完整目录。
	Expanded string `json:"expanded"`
	// Exists 展开后目录是否存在。
	Exists bool `json:"exists"`
	// Duplicate 是否属于某个重复分组。
	Duplicate bool `json:"duplicate"`
}

// ScopeView 是单个作用域 PATH 的前端视图。
type ScopeView struct {
	// Scope "system" / "user"。
	Scope string `json:"scope"`
	// Raw PATH 原始串。
	Raw string `json:"raw"`
	// Kind 值类型名（REG_SZ / REG_EXPAND_SZ）。
	Kind string `json:"kind"`
	// Dead 死条目数。
	Dead int `json:"dead"`
	// Entries 可见条目（系统级在前 = 命令解析优先级顺序）。
	Entries []EntryView `json:"entries"`
}

// CommandView 是关键命令的解析视图。
type CommandView struct {
	Command string `json:"command"`
	Found   bool   `json:"found"`
	First   string `json:"first"`
	Scope   string `json:"scope"`
	Index   int    `json:"index"`
}

// NpmView 是 npm prefix 体检视图。
type NpmView struct {
	// Status ok / notOnPath / unavailable。
	Status string `json:"status"`
	Prefix string `json:"prefix"`
}

// CheckView 是一次体检的前端视图。
type CheckView struct {
	// Scopes 系统级在前（Windows 命令解析系统 PATH 拼在用户 PATH 之前）。
	Scopes []ScopeView `json:"scopes"`
	// Commands 工具链回归结果。
	Commands []CommandView `json:"commands"`
	// Npm npm prefix 体检结果。
	Npm NpmView `json:"npm"`
	// Problems 问题总数。
	Problems int `json:"problems"`
	// At 体检时刻。
	At string `json:"at"`
	// SnapshotPath 本次自动保存的快照路径（空 = 未保存）。
	SnapshotPath string `json:"snapshotPath"`
}

// RunCheck 运行完整体检并返回前端视图，同时自动保存快照（与 check 子命令口径一致）。
func (a *App) RunCheck() (*CheckView, error) {
	system, err := registry.ReadPath(registry.ScopeSystem)
	if err == nil {
		var user *registry.PathData
		user, err = registry.ReadPath(registry.ScopeUser)
		if err == nil {
			var sysVars, usrVars map[string]string
			sysVars, err = registry.ReadStringValues(registry.ScopeSystem)
			if err == nil {
				usrVars, err = registry.ReadStringValues(registry.ScopeUser)
				if err == nil {
					return a.checkAndSnapshot(system, user, detect.ComposeVars(sysVars, usrVars))
				}
			}
		}
	}
	return nil, fmt.Errorf("读取注册表失败: %w", err)
}

// checkAndSnapshot 组装体检视图并保存快照；快照失败不阻断体检。
func (a *App) checkAndSnapshot(system, user *registry.PathData, vars detect.Vars) (*CheckView, error) {
	pathext := detect.PathExtFromVars(vars)
	commands := strings.Split(cli.DefaultCommands, ",")
	rep := detect.Check(system, user, vars, pathext, commands, cli.NpmPrefix())

	dupCount := make(map[string]int)
	for _, g := range rep.Duplicates {
		for _, m := range g.Members {
			dupCount[detect.NormalizeEntry(m.Trimmed)] = len(g.Members)
		}
	}
	view := &CheckView{
		Scopes: []ScopeView{
			buildScopeView(rep, registry.ScopeSystem, rep.Scanned.System, dupCount),
			buildScopeView(rep, registry.ScopeUser, rep.Scanned.User, dupCount),
		},
		Commands: make([]CommandView, 0, len(rep.Commands)),
		Npm:      NpmView{Status: npmStatusName(rep.Npm.Status), Prefix: rep.Npm.Prefix},
		Problems: rep.ProblemCount(),
		At:       time.Now().Format("2006-01-02 15:04:05"),
	}
	for _, c := range rep.Commands {
		view.Commands = append(view.Commands, CommandView{
			Command: c.Command, Found: c.Found, First: c.First,
			Scope: c.FromScope.String(), Index: c.FromIndex + 1,
		})
	}
	if path, err := snapshot.Save(a.snapshotDir, snapshot.FromPathData(system, user, time.Now(), "gui")); err == nil {
		view.SnapshotPath = path
	}
	return view, nil
}

// buildScopeView 组装单个作用域视图。
func buildScopeView(rep *detect.CheckReport, scope registry.Scope, findings []detect.EntryFinding, dupCount map[string]int) ScopeView {
	dead := 0
	entries := make([]EntryView, 0, len(findings))
	for _, f := range findings {
		if !f.Exists {
			dead++
		}
		entries = append(entries, EntryView{
			Position:  f.Index + 1,
			Entry:     f.Entry,
			Expanded:  f.Expanded,
			Exists:    f.Exists,
			Duplicate: dupCount[detect.NormalizeEntry(f.Trimmed)] > 1,
		})
	}
	pd := rep.System
	if scope == registry.ScopeUser {
		pd = rep.User
	}
	return ScopeView{Scope: scope.String(), Raw: pd.Raw, Kind: pd.Kind.String(), Dead: dead, Entries: entries}
}

// npmStatusName 把 npm 体检状态转为前端友好的短名。
func npmStatusName(s detect.NpmStatus) string {
	switch s {
	case detect.NpmOK:
		return "ok"
	case detect.NpmNotOnPath:
		return "notOnPath"
	default:
		return "unavailable"
	}
}

// OpResult 是一次变更操作（清理 / 重排 / 还原）的结果。
type OpResult struct {
	// Backups 本次操作强制产出的备份路径。
	Backups []string `json:"backups"`
	// Detail 结果摘要。
	Detail string `json:"detail"`
}

// Clean 精确移除指定条目（entries 必须与 check 报告的条目原文逐字一致）。
// 提交流水线（复检 → 强制备份 → 写入 → 校验 → 广播）由 internal/ops 统一执行。
func (a *App) Clean(scope string, entries []string) (*OpResult, error) {
	s, ok := registry.ParseScope(scope)
	if !ok {
		return nil, fmt.Errorf("未知作用域 %q", scope)
	}
	pd, err := registry.ReadPath(s)
	if err != nil {
		return nil, mapErr(err)
	}
	plan, err := mutate.RemoveExact(pd, entries)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := a.precheckElevation(s); err != nil {
		return nil, err
	}
	res, err := ops.CommitPath(a.backupDir, []ops.PathChange{{
		Scope: s, OldRaw: pd.Raw, NewRaw: plan.NewRaw, Kind: plan.Kind,
	}}, time.Now())
	if err != nil {
		return nil, withBackups(mapErr(err), res.Backups)
	}
	return &OpResult{
		Backups: res.Backups,
		Detail:  fmt.Sprintf("已移除 %d 条，保留 %d 条，值类型 %s 不变", len(plan.Removed), len(plan.Kept), plan.Kind),
	}, nil
}

// Reorder 按 ordered（非空白条目的新顺序，原文逐字节不变）调整 PATH 顺序。
func (a *App) Reorder(scope string, ordered []string) (*OpResult, error) {
	s, ok := registry.ParseScope(scope)
	if !ok {
		return nil, fmt.Errorf("未知作用域 %q", scope)
	}
	pd, err := registry.ReadPath(s)
	if err != nil {
		return nil, mapErr(err)
	}
	plan, err := mutate.ReorderVisible(pd, ordered)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := a.precheckElevation(s); err != nil {
		return nil, err
	}
	res, err := ops.CommitPath(a.backupDir, []ops.PathChange{{
		Scope: s, OldRaw: pd.Raw, NewRaw: plan.NewRaw, Kind: plan.Kind,
	}}, time.Now())
	if err != nil {
		return nil, withBackups(mapErr(err), res.Backups)
	}
	return &OpResult{
		Backups: res.Backups,
		Detail:  fmt.Sprintf("顺序已调整，条目原文逐字保留，值类型 %s 不变", plan.Kind),
	}, nil
}

// Restore 把 .reg 备份整键写回（自动识别作用域；还原前强制再备份当前值）。
func (a *App) Restore(file string) (*OpResult, error) {
	res, err := ops.RestoreFile(a.backupDir, file, time.Now())
	if err != nil {
		return nil, withBackups(mapErr(err), res.Backups)
	}
	return &OpResult{
		Backups: res.Backups,
		Detail:  fmt.Sprintf("%s级 Environment 已还原 %d 个值（类型以备份为准）", scopeLabel(res.Scope), len(res.Written)),
	}, nil
}

// BackupItem 是备份列表项视图。
type BackupItem struct {
	// Scope "user" / "system"。
	Scope string `json:"scope"`
	// At 备份时刻。
	At time.Time `json:"at"`
	// Path 备份文件完整路径。
	Path string `json:"path"`
}

// ListBackups 列出备份目录中的全部备份（新 → 旧）。
func (a *App) ListBackups() ([]BackupItem, error) {
	infos, err := backup.List(a.backupDir)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]BackupItem, 0, len(infos))
	for _, b := range infos {
		out = append(out, BackupItem{Scope: b.Scope.String(), At: b.At, Path: b.Path})
	}
	return out, nil
}

// BackupNow 立即备份两级 Environment 键，返回备份文件路径。
func (a *App) BackupNow() ([]string, error) {
	now := time.Now()
	var paths []string
	for _, s := range []registry.Scope{registry.ScopeUser, registry.ScopeSystem} {
		p, err := backup.WriteScopeFile(a.backupDir, s, now)
		if err != nil {
			return paths, mapErr(err)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// DiffEntryView 是 diff 中的单个条目变化。
type DiffEntryView struct {
	Position int    `json:"position"`
	Entry    string `json:"entry"`
}

// DiffScopeView 是单个作用域的对比视图。
type DiffScopeView struct {
	Scope        string          `json:"scope"`
	Added        []DiffEntryView `json:"added"`
	Removed      []DiffEntryView `json:"removed"`
	OrderChanged bool            `json:"orderChanged"`
	OldOrder     []string        `json:"oldOrder"`
	NewOrder     []string        `json:"newOrder"`
	KindChanged  bool            `json:"kindChanged"`
	OldKind      string          `json:"oldKind"`
	NewKind      string          `json:"newKind"`
}

// DiffView 是快照对比的前端视图。
type DiffView struct {
	// OldLabel 基准快照文件路径。
	OldLabel string `json:"oldLabel"`
	// NewLabel 对比对象标签（当前注册表）。
	NewLabel string `json:"newLabel"`
	// OldAt / NewAt 两端时刻。
	OldAt string `json:"oldAt"`
	NewAt string `json:"newAt"`
	// Scopes 各作用域变化（用户级在前，与 diff 子命令一致）。
	Scopes []DiffScopeView `json:"scopes"`
	// Changes 变化总数。
	Changes int `json:"changes"`
}

// DiffLatest 对比最新快照与当前注册表（"上次体检以来 PATH 变了什么"）。
func (a *App) DiffLatest() (*DiffView, error) {
	oldSnap, oldPath, err := snapshot.Latest(a.snapshotDir)
	if err != nil {
		return nil, errors.New("还没有体检快照：请先点「体检」生成")
	}
	cur, err := snapshot.Capture(time.Now(), "gui-diff")
	if err != nil {
		return nil, mapErr(err)
	}
	rep := snapshot.Diff(oldSnap, cur)
	view := &DiffView{
		OldLabel: oldPath,
		NewLabel: "当前注册表",
		OldAt:    rep.OldAt.Format("2006-01-02 15:04:05"),
		NewAt:    rep.NewAt.Format("2006-01-02 15:04:05"),
		Changes:  rep.ChangeCount(),
	}
	for _, s := range rep.Scopes {
		sv := DiffScopeView{
			Scope:    s.Scope.String(),
			OldOrder: s.OldOrder,
			NewOrder: s.NewOrder,
			OldKind:  s.OldKind.String(),
			NewKind:  s.NewKind.String(),
		}
		for _, e := range s.Added {
			sv.Added = append(sv.Added, DiffEntryView{Position: e.Position, Entry: e.Entry})
		}
		for _, e := range s.Removed {
			sv.Removed = append(sv.Removed, DiffEntryView{Position: e.Position, Entry: e.Entry})
		}
		view.Scopes = append(view.Scopes, sv)
	}
	return view, nil
}

// precheckElevation 系统级写入前的提权预检：未提权明确报错并提示重启，绝不静默失败。
func (a *App) precheckElevation(s registry.Scope) error {
	if s == registry.ScopeSystem && !registry.IsElevated() {
		return errors.New("系统级 PATH 修改需要管理员权限：请以管理员身份重启后再试")
	}
	return nil
}

// mapErr 把 core/ops 的底层错误翻译为面向 GUI 用户的中文提示。
func mapErr(err error) error {
	switch {
	case errors.Is(err, registry.ErrAccessDenied):
		return errors.New("需要管理员权限：请以管理员身份重启后再试（系统级写入必须提权）")
	case errors.Is(err, ops.ErrPathChanged):
		return errors.New("PATH 在操作期间被外部修改，已中止且未做任何变更。请重新体检")
	case errors.Is(err, ops.ErrVerifyFailed):
		return errors.New("写入后校验失败（结果与计划不一致）。请用自动生成的备份还原")
	case errors.Is(err, mutate.ErrTargetNotFound):
		return errors.New("所选条目与当前 PATH 不一致（可能刚被外部修改）。请重新体检")
	case errors.Is(err, mutate.ErrNotPermutation):
		return errors.New("提交的顺序与当前 PATH 不一致（可能刚被外部修改）。请刷新列表")
	case errors.Is(err, mutate.ErrNoTargets):
		return errors.New("没有要清理的条目")
	case errors.Is(err, ops.ErrUnknownScopeKey):
		return errors.New("备份文件不含已知的 Environment 键，无法还原")
	}
	return err
}

// withBackups 在错误信息中附带备份路径（写入中途失败时给用户明确的还原入口）。
func withBackups(err error, backups []string) error {
	if err == nil || len(backups) == 0 {
		return err
	}
	return fmt.Errorf("%w\n如需还原，双击导入备份：%s", err, strings.Join(backups, "\n"))
}

// scopeLabel 返回作用域中文名（与 CLI 输出一致）。
func scopeLabel(s registry.Scope) string {
	if s == registry.ScopeSystem {
		return "系统"
	}
	return "用户"
}
