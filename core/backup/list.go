package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"envwarden/core/registry"
)

// fileNamePrefix 是本工具生成的备份文件名前缀，也是 List 的识别依据。
const fileNamePrefix = "envwarden-backup-"

// fileNameTimeLayout 是文件名中时间戳部分的布局。
const fileNameTimeLayout = "20060102-150405"

// BackupInfo 描述目录中一份 envwarden 生成的 .reg 备份。
type BackupInfo struct {
	// Scope 备份的作用域。
	Scope registry.Scope
	// At 备份生成时刻（取自文件名时间戳，精度到秒）。
	At time.Time
	// Path 备份文件完整路径。
	Path string
}

// List 列出 dir 下全部 envwarden 生成的备份，按时间新→旧排序。
// 目录不存在视为尚无备份，返回空列表而非错误；命名不符合本工具规则的文件跳过。
func List(dir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list backups: %w", err)
	}
	var out []BackupInfo
	for _, e := range entries {
		info, ok := parseBackupName(e.Name())
		if !ok {
			continue
		}
		info.Path = filepath.Join(dir, e.Name())
		out = append(out, info)
	}
	// 时间新→旧；同秒时按文件名字典序（system > user）稳定排序。
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return filepath.Base(out[i].Path) > filepath.Base(out[j].Path)
	})
	return out, nil
}

// parseBackupName 解析备份文件名为作用域与时间；命名不符或时间非法返回 false。
func parseBackupName(name string) (BackupInfo, bool) {
	if !strings.HasPrefix(name, fileNamePrefix) || !strings.HasSuffix(name, ".reg") {
		return BackupInfo{}, false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(name, fileNamePrefix), ".reg")
	dash := strings.IndexByte(rest, '-')
	if dash < 0 {
		return BackupInfo{}, false
	}
	scope, ok := registry.ParseScope(rest[:dash])
	if !ok {
		return BackupInfo{}, false
	}
	at, err := time.Parse(fileNameTimeLayout, rest[dash+1:])
	if err != nil {
		return BackupInfo{}, false
	}
	return BackupInfo{Scope: scope, At: at}, true
}
