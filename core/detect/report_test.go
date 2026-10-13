package detect

import "testing"

func TestProblemCount(t *testing.T) {
	rep := &CheckReport{
		Dead:       make([]EntryFinding, 2),
		Duplicates: make([]DuplicateGroup, 1),
		Commands:   []CommandResolution{{Command: "a", Found: true}, {Command: "b"}},
		Npm:        NpmResult{Status: NpmNotOnPath},
	}
	if got := rep.ProblemCount(); got != 5 {
		t.Errorf("got %d, want 5（死2 + 重复1 + 缺命令1 + npm1）", got)
	}
	// npm 不可用只警告，不计问题（与 env-check.ps1 口径一致）。
	rep.Npm = NpmResult{Status: NpmUnavailable}
	if got := rep.ProblemCount(); got != 4 {
		t.Errorf("npm 不可用不应计问题: got %d, want 4", got)
	}
}
