package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"envwarden/core/registry"
)

// DirName 是快照存储目录的目录名（当 LOCALAPPDATA 不可用时的回退位置名）。
const DirName = "envwarden-snapshots"

// currentVersion 是快照 JSON 的当前模式版本；更高的版本号视为来自更新版本工具，拒绝加载。
const currentVersion = 1

// ErrNoSnapshot 表示快照存储目录中不存在任何快照。
var ErrNoSnapshot = errors.New("no snapshot found")

// ErrBadSnapshot 表示快照文件内容无法解析（损坏、模式过新等）。
var ErrBadSnapshot = errors.New("bad snapshot file")

// PathSnapshot 是单个作用域 PATH 原始值在快照时刻的完整记录。
type PathSnapshot struct {
	// Scope 作用域，"user" / "system"。
	Scope string `json:"scope"`
	// Raw PATH 原始串（%VAR% 未展开，逐字节保真）。
	Raw string `json:"raw"`
	// Kind 值类型名（REG_SZ / REG_EXPAND_SZ）。
	Kind string `json:"kind"`
}

// Snapshot 是一次体检时刻两级 PATH 的存档，即"病历"。
type Snapshot struct {
	// Version 模式版本。
	Version int `json:"version"`
	// CreatedAt 快照创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// Source 快照来源标识（如 check / diff-current）。
	Source string `json:"source,omitempty"`
	// Paths 两级 PATH 记录，用户级在前。
	Paths []PathSnapshot `json:"paths"`
}

// FromPathData 由刚读取的两级 PathData 构造快照；
// 复用体检链已读数据，避免快照与体检结果不一致。
func FromPathData(system, user *registry.PathData, at time.Time, source string) *Snapshot {
	return &Snapshot{
		Version:   currentVersion,
		CreatedAt: at,
		Source:    source,
		Paths: []PathSnapshot{
			fromOne(user),
			fromOne(system),
		},
	}
}

// fromOne 转换单个作用域；读取失败（理论上不会）时保留空记录。
func fromOne(pd *registry.PathData) PathSnapshot {
	if pd == nil {
		return PathSnapshot{Scope: registry.ScopeUser.String()}
	}
	return PathSnapshot{Scope: pd.Scope.String(), Raw: pd.Raw, Kind: pd.Kind.String()}
}

// Capture 实时读取两级注册表 PATH 构造快照（"新终端视角"）。
func Capture(at time.Time, source string) (*Snapshot, error) {
	system, err := registry.ReadPath(registry.ScopeSystem)
	if err == nil {
		var user *registry.PathData
		user, err = registry.ReadPath(registry.ScopeUser)
		if err == nil {
			return FromPathData(system, user, at, source), nil
		}
	}
	return nil, fmt.Errorf("capture snapshot: %w", err)
}

// Path 提取指定作用域的 PATH 记录；快照缺失该作用域时返回错误。
func (s *Snapshot) Path(scope registry.Scope) (PathSnapshot, error) {
	for _, p := range s.Paths {
		if p.Scope == scope.String() {
			return p, nil
		}
	}
	return PathSnapshot{}, fmt.Errorf("snapshot %s: %w: missing scope %s",
		s.CreatedAt.Format(time.RFC3339), ErrBadSnapshot, scope)
}

// ToPathData 把单个作用域记录还原为 PathData，供对比逻辑复用注册表切分规则。
func (p PathSnapshot) ToPathData() (*registry.PathData, error) {
	scope, ok := registry.ParseScope(p.Scope)
	if !ok {
		return nil, fmt.Errorf("snapshot path: %w: unknown scope %q", ErrBadSnapshot, p.Scope)
	}
	kind, err := registry.ParseValueKind(p.Kind)
	if err != nil {
		return nil, fmt.Errorf("snapshot path %s: %w: %v", p.Scope, ErrBadSnapshot, err)
	}
	pd, err := registry.NewPathData(scope, p.Raw, kind)
	if err != nil {
		return nil, fmt.Errorf("snapshot path %s: %w: %v", p.Scope, ErrBadSnapshot, err)
	}
	return pd, nil
}

// Save 将快照写入 dir，文件名内嵌时间戳（snapshot-20060102-150405[.N].json），
// 同秒冲突时追加序号；目录不存在时自动创建。返回写入的文件路径。
func Save(dir string, s *Snapshot) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("save snapshot: mkdir %s: %w", dir, err)
	}
	base := "snapshot-" + s.CreatedAt.Format("20060102-150405")
	path := filepath.Join(dir, base+".json")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s.%d.json", base, i))
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("save snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("save snapshot: %s: %w", path, err)
	}
	return path, nil
}

// Load 读取并校验单个快照文件。
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("load snapshot %s: %w: %v", path, ErrBadSnapshot, err)
	}
	if s.Version <= 0 || s.Version > currentVersion {
		return nil, fmt.Errorf("load snapshot %s: %w: version %d > %d（由更新版本工具创建）",
			path, ErrBadSnapshot, s.Version, currentVersion)
	}
	if len(s.Paths) == 0 {
		return nil, fmt.Errorf("load snapshot %s: %w: no paths", path, ErrBadSnapshot)
	}
	return &s, nil
}

// Latest 返回 dir 中最新的快照（按文件名字典序，时间戳固定宽度保证排序正确）。
// 目录不存在或为空时返回包装 ErrNoSnapshot 的错误。
func Latest(dir string) (*Snapshot, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("latest snapshot: %w: %v", ErrNoSnapshot, err)
	}
	var newest string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "snapshot-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if name > newest {
			newest = name
		}
	}
	if newest == "" {
		return nil, "", fmt.Errorf("latest snapshot in %s: %w", dir, ErrNoSnapshot)
	}
	path := filepath.Join(dir, newest)
	s, err := Load(path)
	if err != nil {
		return nil, "", err
	}
	return s, path, nil
}
