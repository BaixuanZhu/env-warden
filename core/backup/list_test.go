package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"envwarden/core/registry"
)

func TestParseBackupName(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantScope registry.Scope
		wantAt    time.Time
		wantOK    bool
	}{
		{
			name:      "用户级备份",
			file:      "envwarden-backup-user-20261014-153001.reg",
			wantScope: registry.ScopeUser,
			wantAt:    time.Date(2026, 10, 14, 15, 30, 1, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "系统级备份",
			file:      "envwarden-backup-system-20260101-000000.reg",
			wantScope: registry.ScopeSystem,
			wantAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{name: "非本工具命名", file: "HKCU-Environment.reg", wantOK: false},
		{name: "作用域非法", file: "envwarden-backup-global-20261014-153001.reg", wantOK: false},
		{name: "时间戳非法", file: "envwarden-backup-user-2026-10-14.reg", wantOK: false},
		{name: "缺后缀", file: "envwarden-backup-user-20261014-153001", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseBackupName(tt.file)
			if ok != tt.wantOK {
				t.Fatalf("parseBackupName(%q) ok = %v, want %v", tt.file, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Scope != tt.wantScope {
				t.Errorf("Scope = %s, want %s", got.Scope, tt.wantScope)
			}
			if !got.At.Equal(tt.wantAt) {
				t.Errorf("At = %v, want %v", got.At, tt.wantAt)
			}
		})
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"envwarden-backup-user-20261014-153001.reg",
		"envwarden-backup-user-20261014-090000.reg",
		"envwarden-backup-system-20261014-153001.reg",
		"unrelated.txt",
		"HKCU-Environment.reg",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List 返回 %d 份备份, want 3（无关文件跳过）", len(got))
	}
	// 时间新→旧；同秒的两份按文件名字典序（user > system）稳定排列。
	wantOrder := []string{
		"envwarden-backup-user-20261014-153001.reg",
		"envwarden-backup-system-20261014-153001.reg",
		"envwarden-backup-user-20261014-090000.reg",
	}
	for i, w := range wantOrder {
		if filepath.Base(got[i].Path) != w {
			t.Errorf("List[%d] = %s, want %s", i, filepath.Base(got[i].Path), w)
		}
	}

	t.Run("目录不存在返回空列表", func(t *testing.T) {
		got, err := List(filepath.Join(dir, "no-such-dir"))
		if err != nil {
			t.Fatalf("List 不存在的目录不应报错: %v", err)
		}
		if got != nil {
			t.Errorf("List = %v, want nil", got)
		}
	})
}
