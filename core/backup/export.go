package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"envwarden/core/registry"
)

// BackupDirName 是备份文件的默认目录名（相对当前目录创建）。
const BackupDirName = "envwarden-backups"

// FileName 生成单个作用域的备份文件名，如 envwarden-backup-user-20261014-153001.reg。
func FileName(scope registry.Scope, now time.Time) string {
	return fmt.Sprintf("envwarden-backup-%s-%s.reg", scope, now.Format("20060102-150405"))
}

// WriteScopeFile 读取作用域键下全部值，序列化为 regedit 5.00 格式并写入 dir 下的备份文件。
// 返回写出的文件路径。文件可直接双击导入还原（系统级导入时 regedit 会自动请求 UAC）。
func WriteScopeFile(dir string, scope registry.Scope, now time.Time) (string, error) {
	content, err := ExportScope(scope)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("write backup file: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName(scope, now))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", fmt.Errorf("write backup file: %s: %w", path, err)
	}
	return path, nil
}

// ExportScope 读取作用域键下全部值并序列化为 .reg 备份内容（UTF-16LE + BOM）。
func ExportScope(scope registry.Scope) ([]byte, error) {
	values, err := registry.ReadAllValues(scope)
	if err != nil {
		return nil, err
	}
	return FormatRegFile(scope, values), nil
}

// FormatRegFile 将键值序列化为 regedit 5.00 导出格式的字节流：
// UTF-16LE + BOM、CRLF 行尾、hex 数据按 regedit 列宽续行，保证双击可导入。
func FormatRegFile(scope registry.Scope, values []registry.RegValue) []byte {
	var b strings.Builder
	b.WriteString("Windows Registry Editor Version 5.00\r\n\r\n")
	b.WriteString("[" + scope.KeyPath() + "]\r\n")
	for _, v := range values {
		writeValue(&b, v)
	}
	b.WriteString("\r\n") // 文件以空行收尾，与 regedit 导出一致
	return encodeUTF16LE(b.String())
}

// writeValue 写入单个值行；字符串值做 \\ 与 \" 转义，hex 类数据走续行规则。
func writeValue(b *strings.Builder, v registry.RegValue) {
	nameEq := quoteReg(v.Name) + "="
	switch v.Kind {
	case registry.KindSZ:
		b.WriteString(nameEq)
		b.WriteString(quoteReg(v.Str))
		b.WriteString("\r\n")
	case registry.KindExpandSZ:
		// hex(2) 数据是含单个 NUL 终止符的 UTF-16LE 字符串（值数据不带 BOM）。
		wrapHexLine(b, nameEq+"hex(2):", utf16LEBytes(v.Str+"\x00"))
	case registry.KindDWORD:
		b.WriteString(nameEq)
		fmt.Fprintf(b, "dword:%08x\r\n", v.Dword)
	case registry.KindMultiSZ:
		wrapHexLine(b, nameEq+"hex(7):", v.Raw)
	case registry.KindQWord:
		wrapHexLine(b, nameEq+"hex(b):", v.Raw)
	default:
		// REG_BINARY、REG_NONE 及未知类型按原始字节导出，还原时不丢信息。
		wrapHexLine(b, nameEq+"hex:", v.Raw)
	}
}

// quoteReg 按注册表字符串转义规则加上引号：\ 与 " 前补反斜杠。
func quoteReg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// wrapHexLine 输出 header + 十六进制逗号序列，超宽时按 regedit 规则续行：
// 首行宽（含行尾 '\'）不超过 firstMax，续行以两个空格缩进、宽不超过 contMax。
// 列宽与真实导出对齐，保证 regedit 双击导入时的兼容性。
func wrapHexLine(b *strings.Builder, header string, data []byte) {
	const firstMax = 80
	const contMax = 78
	b.WriteString(header)
	cur := len(header) // 当前行已占列数（header 计入首行）
	max := firstMax
	for i, by := range data {
		tok := fmt.Sprintf("%02x", by)
		if i < len(data)-1 {
			tok += ","
		}
		if cur+len(tok)+1 > max { // +1 预留行尾 '\'
			b.WriteString("\\\r\n  ")
			cur = 2
			max = contMax
		}
		b.WriteString(tok)
		cur += len(tok)
	}
	b.WriteString("\r\n")
}

// utf16LEBytes 编码为无 BOM 的 UTF-16LE 字节流，用于 hex(2)/hex(7) 值数据。
func utf16LEBytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2*len(u))
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// encodeUTF16LE 在 UTF-16LE 字节流前置 regedit 5.00 要求的 BOM，用于整个文件编码。
func encodeUTF16LE(s string) []byte {
	return append([]byte{0xFF, 0xFE}, utf16LEBytes(s)...)
}
