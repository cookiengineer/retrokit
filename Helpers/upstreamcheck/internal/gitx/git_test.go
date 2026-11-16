package gitx

import "testing"

func TestParseNameStatus(t *testing.T) {
	out := "M\tSource/A.cpp\nD\tSource/B.cpp\nR098\tSource/Old.cpp\tSource/New.cpp\n"
	entries := parseNameStatus(out)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Status != 'M' || entries[0].NewPath != "Source/A.cpp" {
		t.Fatalf("unexpected modified entry: %#v", entries[0])
	}
	if entries[2].Status != 'R' || entries[2].OldPath != "Source/Old.cpp" || entries[2].NewPath != "Source/New.cpp" {
		t.Fatalf("unexpected rename entry: %#v", entries[2])
	}
}
