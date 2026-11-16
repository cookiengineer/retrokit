package check

import (
	"testing"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
)

func TestCoveragePaths(t *testing.T) {
	if got := coveragePaths(gitx.DiffEntry{Status: 'D', OldPath: "a"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("delete paths: %#v", got)
	}
	if got := coveragePaths(gitx.DiffEntry{Status: 'R', OldPath: "a", NewPath: "b"}); len(got) != 2 {
		t.Fatalf("rename paths: %#v", got)
	}
	if got := coveragePaths(gitx.DiffEntry{Status: 'M', NewPath: "b"}); len(got) != 1 || got[0] != "b" {
		t.Fatalf("modify paths: %#v", got)
	}
}
