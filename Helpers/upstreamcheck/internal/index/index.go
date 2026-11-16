package index

import (
	"path/filepath"
	"sort"
	"strings"
)

// Symbol is a named declaration found in a source file. Start and End are
// byte offsets into the file, which lets the migration code remove a
// declaration without re-parsing it later.
type Symbol struct {
	Name      string
	Kind      string
	Path      string
	Start     int
	End       int
	StartLine int
	EndLine   int
}

// Line is a significant line in a build or configuration file.
type Line struct {
	Number int
	Text   string
}

type FileIndex struct {
	Path    string
	Symbols []Symbol
	Lines   []Line
}

// IndexFile dispatches to the parser appropriate for the file type.
func IndexFile(path string, content []byte) FileIndex {
	path = filepath.ToSlash(path)
	switch {
	case IsCpp(path):
		return FileIndex{Path: path, Symbols: IndexCpp(path, content)}
	case IsIDL(path):
		return FileIndex{Path: path, Symbols: IndexIDL(path, content)}
	case IsBuild(path):
		return FileIndex{Path: path, Lines: BuildLines(content)}
	default:
		return FileIndex{Path: path}
	}
}

func IsCpp(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".m", ".mm":
		return true
	}
	return false
}

func IsIDL(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".idl")
}

func IsBuild(path string) bool {
	base := filepath.Base(path)
	if base == "CMakeLists.txt" || base == "Sources.txt" || base == "Headers.cmake" ||
		base == "DerivedSources.make" || strings.HasPrefix(base, "Sources") && strings.HasSuffix(base, ".txt") {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmake", ".in", ".yaml", ".yml", ".json", ".make":
		return true
	}
	return false
}

// SymbolSet maps a stable key (kind + name) to a symbol.
func SymbolSet(symbols []Symbol) map[string]Symbol {
	result := make(map[string]Symbol, len(symbols))
	for _, sym := range symbols {
		result[sym.Kind+"\x00"+sym.Name] = sym
	}
	return result
}

// LinesSet maps significant line text to its first occurrence.
func LinesSet(lines []Line) map[string]Line {
	result := make(map[string]Line, len(lines))
	for _, line := range lines {
		result[line.Text] = line
	}
	return result
}

// DiffLines returns removed and added lines in stable file order. Duplicate
// lines are handled as a multiset so the manifest does not depend on map
// iteration order.
func DiffLines(base, fork []Line) (removed, added []Line) {
	forkCount := make(map[string]int, len(fork))
	for _, line := range fork {
		forkCount[line.Text]++
	}
	for _, line := range base {
		if forkCount[line.Text] > 0 {
			forkCount[line.Text]--
			continue
		}
		removed = append(removed, line)
	}
	baseCount := make(map[string]int, len(base))
	for _, line := range base {
		baseCount[line.Text]++
	}
	for _, line := range fork {
		if baseCount[line.Text] > 0 {
			baseCount[line.Text]--
			continue
		}
		added = append(added, line)
	}
	return removed, added
}

// SortSymbols orders symbols by position for stable reports.
func SortSymbols(symbols []Symbol) {
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].Start == symbols[j].Start {
			return symbols[i].Name < symbols[j].Name
		}
		return symbols[i].Start < symbols[j].Start
	})
}
