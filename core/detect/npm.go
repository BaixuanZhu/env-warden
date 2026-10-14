package detect

// NpmStatus npm 全局 prefix 体检结论。
type NpmStatus int

const (
	// NpmOK prefix 存在且已在两级 PATH 上。
	NpmOK NpmStatus = iota
	// NpmNotOnPath prefix 存在但不在 PATH 上，npm -g 安装的工具将不可用。
	NpmNotOnPath
	// NpmUnavailable 无法确定 prefix（npm 未安装、异常或 prefix 目录不存在）。
	NpmUnavailable
)

// NpmResult npm 全局 prefix 体检结果。
type NpmResult struct {
	Status NpmStatus
	Prefix string // 体检时的 npm prefix（可能为空）
}

// CheckNpmPrefix 判断 npm 全局 prefix 是否出现在两级 PATH 上。
//
// 比对口径与 env-check.ps1 一致：条目与 prefix 均做 TrimSpace + 去尾反斜杠 + 小写
// 归一化后等值比较；prefix 为空或目录不存在时视为不可用（只警告，不计问题）。
func CheckNpmPrefix(prefix string, scanned *ScannedEntries) NpmResult {
	if prefix == "" {
		return NpmResult{Status: NpmUnavailable}
	}
	if !dirExists(prefix) {
		return NpmResult{Status: NpmUnavailable, Prefix: prefix}
	}
	want := NormalizeEntry(prefix)
	for _, f := range scanned.All() {
		if NormalizeEntry(f.Expanded) == want {
			return NpmResult{Status: NpmOK, Prefix: prefix}
		}
	}
	return NpmResult{Status: NpmNotOnPath, Prefix: prefix}
}
