package check

import (
	"testing"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/index"
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

func TestIsCommentLinePreprocessor(t *testing.T) {
	cases := []struct {
		path    string
		content string
		want    bool
	}{
		{"Source/Foo.cpp", "#if ENABLE(FEATURE)", false},
		{"Source/Foo.cpp", "#include <wtf/Platform.h>", false},
		{"Source/Foo.cpp", "  #define FEATURE 1", false},
		{"Source/Foo.h", "#endif", false},
		{"Source/Foo.serialization.in", "#if ENABLE(FEATURE)", false},
		{"Source/Foo.cpp", "# plain hash comment", true},
		{"Source/Foo.cpp", "// comment", true},
		{"Source/CMakeLists.txt", "#if looks like a directive but is a comment", true},
		{"Source/script.py", "#if this is a python comment", true},
		{"Source/foo.yaml", "#if this is a yaml comment", true},
		{"Source/Foo.cpp", "int x = 1;", false},
	}
	for _, c := range cases {
		if got := isCommentLine(c.path, c.content); got != c.want {
			t.Errorf("isCommentLine(%q, %q) = %v, want %v", c.path, c.content, got, c.want)
		}
	}
}

func TestLineEntryPresent(t *testing.T) {
	lines := index.LinesSet([]index.Line{
		{Number: 1, Text: "NetworkProcess/CustomProtocols/Cocoa/LegacyCustomProtocolManagerCocoa.mm @nonARC"},
		{Number: 2, Text: "NetworkProcess/CustomProtocols/LegacyCustomProtocolManager.cpp @cost:3"},
		{Number: 3, Text: "NetworkProcess/CustomProtocols/LegacyCustomProtocolManager \\"},
	})
	if !lineEntryPresent("build_ref", lines, "NetworkProcess/CustomProtocols/Cocoa/LegacyCustomProtocolManagerCocoa.mm") {
		t.Error("annotated build_ref should still be present")
	}
	if !lineEntryPresent("build_ref", lines, "NetworkProcess/CustomProtocols/LegacyCustomProtocolManager.cpp") {
		t.Error("cost-annotated build_ref should still be present")
	}
	if !lineEntryPresent("build_ref", lines, "NetworkProcess/CustomProtocols/LegacyCustomProtocolManager \\") {
		t.Error("continuation build_ref should still be present")
	}
	if lineEntryPresent("build_ref", lines, "NetworkProcess/CustomProtocols/Missing.cpp") {
		t.Error("absent build_ref reported as present")
	}
	if lineEntryPresent("flag", lines, "networkprocess/customprotocols/cocoa/legacycustomprotocolmanagercocoa.mm") {
		t.Error("non build_ref entries must match exactly")
	}
}
