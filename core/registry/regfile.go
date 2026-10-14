package registry

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// RegValue 表示 .reg 文件中的一个命名值。
// 字符串类值（REG_SZ / REG_EXPAND_SZ）解码进 Str（REG_EXPAND_SZ 不展开 %VAR%）；
// 其余类型保留 Raw 原始字节，REG_DWORD 另存 Dword。
type RegValue struct {
	Name  string
	Kind  ValueKind
	Str   string
	Raw   []byte
	Dword uint32
}

// RegFile 表示一个 regedit 导出文件（版本 5.00）解析后的全部键值。
type RegFile struct {
	// Keys 以规范化键路径（小写）索引该键下的值列表。
	Keys map[string][]RegValue
}

// Value 返回 keyPath 键下名为 name 的值；键路径与值名比较均不区分大小写。
func (f *RegFile) Value(keyPath, name string) (RegValue, bool) {
	for _, v := range f.Keys[strings.ToLower(keyPath)] {
		if strings.EqualFold(v.Name, name) {
			return v, true
		}
	}
	return RegValue{}, false
}

// PathValue 返回作用域对应键下的 Path 值。
func (f *RegFile) PathValue(scope Scope) (RegValue, bool) {
	return f.Value(scope.KeyPath(), valueNamePath)
}

// ParseRegFile 解析 regedit 导出的 .reg 文件内容。
// 支持 UTF-16LE + BOM（regedit 5.00 标准格式）与 UTF-8；
// 旧版 ANSI（REGEDIT4）等编码返回错误。
func ParseRegFile(data []byte) (*RegFile, error) {
	text, err := decodeRegText(data)
	if err != nil {
		return nil, err
	}
	lines := splitLines(text)
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "Windows Registry Editor Version") {
		return nil, fmt.Errorf("ParseRegFile: 无法识别的文件头（期望 regedit 5.00 导出格式）")
	}

	f := &RegFile{Keys: make(map[string][]RegValue)}
	curKey := ""
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("ParseRegFile: 键行缺少 ']': %q", line)
			}
			curKey = strings.ToLower(line[1 : len(line)-1])
			continue
		}
		if curKey == "" {
			return nil, fmt.Errorf("ParseRegFile: 值行出现在任何键之前: %q", line)
		}
		// 以单个 '\' 结尾的行是续行，与下一行合并后再解析（hex 数据常见）。
		for strings.HasSuffix(line, `\`) && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, `\`) + strings.TrimSpace(lines[i])
		}
		v, err := parseValueLine(line)
		if err != nil {
			return nil, err
		}
		f.Keys[curKey] = append(f.Keys[curKey], v)
	}
	return f, nil
}

// parseValueLine 解析形如 "Name"=... 的单个逻辑行（续行已合并）。
func parseValueLine(line string) (RegValue, error) {
	name, rest, err := scanQuotedName(line)
	if err != nil {
		return RegValue{}, err
	}
	if !strings.HasPrefix(rest, "=") {
		return RegValue{}, fmt.Errorf("ParseRegFile: 值行缺少 '=': %q", line)
	}
	body := rest[1:]
	switch {
	case strings.HasPrefix(body, `"`):
		s, err := unescapeRegString(body)
		if err != nil {
			return RegValue{}, err
		}
		return RegValue{Name: name, Kind: KindSZ, Str: s}, nil
	case strings.HasPrefix(body, "dword:"):
		u, err := strconv.ParseUint(strings.TrimPrefix(body, "dword:"), 16, 32)
		if err != nil {
			return RegValue{}, fmt.Errorf("ParseRegFile: dword 解析失败: %w", err)
		}
		return RegValue{Name: name, Kind: KindDWORD, Dword: uint32(u)}, nil
	case strings.HasPrefix(body, "hex:"):
		data, err := parseHexData(strings.TrimPrefix(body, "hex:"))
		if err != nil {
			return RegValue{}, err
		}
		return RegValue{Name: name, Kind: KindBinary, Raw: data}, nil
	case strings.HasPrefix(body, "hex("):
		closeIdx := strings.IndexByte(body, ')')
		if closeIdx < 0 || !strings.HasPrefix(body[closeIdx:], "):") {
			return RegValue{}, fmt.Errorf("ParseRegFile: hex(N) 语法错误: %q", line)
		}
		// 类型号按十六进制解析：REG_QWORD 等两位数类型 regedit 以 hex(b) 形式导出。
		kindNum, err := strconv.ParseUint(body[4:closeIdx], 16, 32)
		if err != nil {
			return RegValue{}, fmt.Errorf("ParseRegFile: hex(N) 类型号解析失败: %w", err)
		}
		data, err := parseHexData(body[closeIdx+2:])
		if err != nil {
			return RegValue{}, err
		}
		v := RegValue{Name: name, Kind: ValueKind(kindNum), Raw: data}
		if v.Kind == KindExpandSZ {
			// hex(2) 数据是含 NUL 终止符的 UTF-16LE 字符串。
			v.Str = strings.TrimRight(decodeUTF16LE(data), "\x00")
		}
		return v, nil
	default:
		return RegValue{}, fmt.Errorf("ParseRegFile: 不支持的值格式: %q", line)
	}
}

// scanQuotedName 解析值行开头带引号的值名，返回反转义后的名字与剩余部分（以 '=' 开头）。
func scanQuotedName(line string) (name string, rest string, err error) {
	if !strings.HasPrefix(line, `"`) {
		return "", "", fmt.Errorf("ParseRegFile: 值行必须以引号值名开头: %q", line)
	}
	var b strings.Builder
	for i := 1; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++
			b.WriteByte(line[i]) // \\ -> \ ， \" -> "
			continue
		}
		if c == '"' {
			return b.String(), line[i+1:], nil
		}
		b.WriteByte(c)
	}
	return "", "", fmt.Errorf("ParseRegFile: 值名缺少闭合引号: %q", line)
}

// unescapeRegString 解析带引号的 REG_SZ 字符串字面量，返回反转义后的内容。
func unescapeRegString(s string) (string, error) {
	if !strings.HasPrefix(s, `"`) {
		return "", fmt.Errorf("ParseRegFile: 字符串值必须以引号开头: %q", s)
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		if c == '"' {
			return b.String(), nil
		}
		b.WriteByte(c)
	}
	return "", fmt.Errorf("ParseRegFile: 字符串值缺少闭合引号: %q", s)
}

// parseHexData 解析形如 "25,00,5c,00" 的十六进制字节序列。
func parseHexData(s string) ([]byte, error) {
	parts := strings.Split(s, ",")
	out := make([]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("ParseRegFile: 十六进制字节 %q 解析失败: %w", p, err)
		}
		out = append(out, byte(b))
	}
	return out, nil
}

// decodeRegText 按文件头 BOM 解码 .reg 文本：UTF-16LE（regedit 5.00 标准）或 UTF-8。
func decodeRegText(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16LE(data[2:]), nil
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:]), nil
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return "", fmt.Errorf("ParseRegFile: 不支持 UTF-16BE 编码")
	default:
		return string(data), nil
	}
}

// decodeUTF16LE 将 UTF-16LE 字节序列解码为字符串；奇数长度时丢弃末尾残字节。
func decodeUTF16LE(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// splitLines 统一按 '\n' 切分并去除 '\r' 残留。
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n")
}
