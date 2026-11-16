package manifest

import "testing"

func TestDedup(t *testing.T) {
	entries := []Entry{
		{Feature: "geolocation", Kind: "file", Symbol: "-", Path: "A.cpp", Disposition: "removed", Origin: "a"},
		{Feature: "geolocation", Kind: "file", Symbol: "-", Path: "A.cpp", Disposition: "removed", Origin: "b", Notes: "second"},
		{Feature: "geolocation", Kind: "method", Symbol: "Geolocation::getCurrentPosition", Path: "A.cpp", Disposition: "removed"},
	}
	result := Dedup(entries)
	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2: %#v", len(result), result)
	}
	if result[0].Origin != "a" || result[0].Notes != "second" {
		t.Fatalf("dedup did not merge metadata: %#v", result[0])
	}
}

func TestFeatureRoundTrip(t *testing.T) {
	path := t.TempDir() + "/features.tsv"
	features := []Feature{
		{ID: "webgl", Title: "WebGL API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEBGL", "WebGL"}},
	}
	if err := WriteFeatures(path, features); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFeatures(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != "webgl" || len(loaded[0].Tokens) != 2 {
		t.Fatalf("unexpected round trip: %#v", loaded)
	}
}
