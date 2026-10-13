package detect

import (
	"testing"

	"envwarden/core/registry"
)

func TestCheckNpmPrefix(t *testing.T) {
	onPath := t.TempDir()  // 在 PATH 上的目录
	offPath := t.TempDir() // 存在但不在 PATH 上

	usr := mustPathData(t, registry.ScopeUser, onPath+`\`)
	scanned := ScanEntries(nil, usr, nil)

	tests := []struct {
		name   string
		prefix string
		want   NpmStatus
	}{
		{"prefix 命中（尾部反斜杠差异归一化）", onPath, NpmOK},
		{"prefix 存在但不在 PATH 上", offPath, NpmNotOnPath},
		{"prefix 为空", "", NpmUnavailable},
		{"prefix 目录不存在", onPath + `\no-such-dir`, NpmUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckNpmPrefix(tt.prefix, scanned).Status; got != tt.want {
				t.Errorf("CheckNpmPrefix(%q): got %d, want %d", tt.prefix, got, tt.want)
			}
		})
	}
}
