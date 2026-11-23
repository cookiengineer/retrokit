package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/index"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/manifest"
)

type StatusResult struct {
	BaseRef       string
	ForkRef       string
	TargetRef     string
	ChangedPaths  int
	InScopePaths  int
	OutOfScope    int
	ManifestRows  int
	Features      int
	Unmapped      []string
	Counts        map[string]map[string]int
	TargetPresent int
}

type LintIssue struct {
	Kind    string
	Message string
	Entry   *manifest.Entry
}

type LintResult struct {
	Errors   []LintIssue
	Warnings []LintIssue
	Checked  int
}

type PlanEntry struct {
	Entry  manifest.Entry
	Status string
	Detail string
}

type PlanFeature struct {
	Feature string
	Title   string
	Pending int
	Done    int
	Manual  int
	Entries []PlanEntry
}

type PlanResult struct {
	TargetRef string
	Features  []PlanFeature
}

func LoadManifest(cfg *config.Config) ([]manifest.Entry, []manifest.Feature, error) {
	entries, err := manifest.LoadEntries(cfg.ManifestPath(cfg.Manifest.Divergence))
	if err != nil {
		return nil, nil, err
	}
	features, err := manifest.LoadFeatures(cfg.ManifestPath(cfg.Manifest.Features))
	if err != nil {
		return nil, nil, err
	}
	return entries, features, nil
}

func Status(repo *gitx.Repo, cfg *config.Config, baseRef, forkRef, targetRef string) (*StatusResult, error) {
	entries, features, err := LoadManifest(cfg)
	if err != nil {
		return nil, err
	}
	diff, err := repo.DiffNameStatus(baseRef, forkRef, nil)
	if err != nil {
		return nil, err
	}

	result := &StatusResult{
		BaseRef:      baseRef,
		ForkRef:      forkRef,
		TargetRef:    targetRef,
		ChangedPaths: len(diff),
		ManifestRows: len(entries),
		Features:     len(features),
		Counts:       manifest.CountByFeature(entries),
	}

	coverage := make(map[string]bool)
	for _, entry := range entries {
		if entry.Kind == "file" && cfg.InScope(entry.Path) {
			coverage[entry.Path] = true
		}
	}
	for _, d := range diff {
		paths := coveragePaths(d)
		inScope := false
		covered := false
		first := ""
		for _, path := range paths {
			if !cfg.InScope(path) {
				continue
			}
			inScope = true
			if first == "" {
				first = path
			}
			if coverage[path] {
				covered = true
			}
		}
		if !inScope {
			result.OutOfScope++
			continue
		}
		result.InScopePaths++
		if !covered {
			result.Unmapped = append(result.Unmapped, first)
		}
	}

	if targetRef != "" && repo.RefExists(targetRef) {
		plan, err := Plan(repo, cfg, baseRef, targetRef)
		if err == nil {
			for _, feature := range plan.Features {
				result.TargetPresent += feature.Pending
			}
		}
	}
	sort.Strings(result.Unmapped)
	return result, nil
}

func Lint(repo *gitx.Repo, cfg *config.Config, baseRef, forkRef string) (*LintResult, error) {
	entries, features, err := LoadManifest(cfg)
	if err != nil {
		return nil, err
	}
	diff, err := repo.DiffNameStatus(baseRef, forkRef, nil)
	if err != nil {
		return nil, err
	}
	result := &LintResult{}

	coverage := make(map[string]bool)
	for _, entry := range entries {
		if entry.Kind == "file" && cfg.InScope(entry.Path) {
			coverage[entry.Path] = true
		}
	}
	for _, d := range diff {
		paths := coveragePaths(d)
		inScope := false
		covered := false
		first := ""
		for _, path := range paths {
			if !cfg.InScope(path) {
				continue
			}
			inScope = true
			if first == "" {
				first = path
			}
			if coverage[path] {
				covered = true
			}
		}
		if !inScope {
			continue
		}
		result.Checked++
		if !covered {
			result.Errors = append(result.Errors, LintIssue{
				Kind:    "unmapped-divergence",
				Message: fmt.Sprintf("%s changed since %s but has no file-level manifest entry", first, baseRef),
			})
		}
	}

	featureSet := make(map[string]bool)
	for _, feature := range features {
		featureSet[feature.ID] = true
	}

	indexCache := make(map[string]index.FileIndex)
	getIndex := func(path string) (index.FileIndex, bool) {
		if cached, ok := indexCache[path]; ok {
			return cached, true
		}
		content, err := repo.ShowFile(forkRef, path)
		if err != nil {
			return index.FileIndex{}, false
		}
		idx := index.IndexFile(path, content)
		indexCache[path] = idx
		return idx, true
	}

	for i := range entries {
		entry := &entries[i]
		if !cfg.InScope(entry.Path) {
			result.Warnings = append(result.Warnings, LintIssue{
				Kind:    "out-of-scope-manifest-entry",
				Message: fmt.Sprintf("%s %s is outside the enforced vendor scope", entry.Kind, entry.Path),
				Entry:   entry,
			})
			continue
		}
		if entry.Feature != "" && !featureSet[entry.Feature] {
			result.Warnings = append(result.Warnings, LintIssue{
				Kind:    "unknown-feature",
				Message: fmt.Sprintf("%s references unknown feature %q", entry.Path, entry.Feature),
				Entry:   entry,
			})
		}
		if entry.Disposition == "retained" {
			exists := repo.FileExistsAt(forkRef, entry.Path)
			idx, indexed := index.FileIndex{}, false
			if exists && entry.Kind != "file" {
				idx, indexed = getIndex(entry.Path)
			}
			if issue := retainedIssue(entry, exists, idx, indexed, forkRef); issue != nil {
				result.Errors = append(result.Errors, *issue)
			}
			continue
		}
		if entry.Disposition != "removed" {
			continue
		}

		if entry.Kind == "file" {
			if repo.FileExistsAt(forkRef, entry.Path) {
				result.Errors = append(result.Errors, LintIssue{
					Kind:    "reappeared-file",
					Message: fmt.Sprintf("%s is marked removed but still exists at %s", entry.Path, forkRef),
					Entry:   entry,
				})
			}
			continue
		}

		if !repo.FileExistsAt(forkRef, entry.Path) {
			continue
		}

		if entry.Kind == "build_ref" || entry.Kind == "flag" || entry.Kind == "pref" || entry.Kind == "config_line" {
			// Build/config lines are guidance for migration, not a unique
			// declaration. Identical lines can legitimately remain elsewhere
			// in the same file, so absence is not a valid lint invariant.
			continue
		}

		idx, ok := getIndex(entry.Path)
		if !ok {
			continue
		}
		if containsSymbol(idx.Symbols, entry.Symbol) {
			result.Errors = append(result.Errors, LintIssue{
				Kind:    "reappeared-symbol",
				Message: fmt.Sprintf("%s is marked removed but still exists in %s", entry.Symbol, entry.Path),
				Entry:   entry,
			})
		}
	}

	return result, nil
}

func Plan(repo *gitx.Repo, cfg *config.Config, baseRef, targetRef string) (*PlanResult, error) {
	entries, features, err := LoadManifest(cfg)
	if err != nil {
		return nil, err
	}
	if !repo.RefExists(targetRef) {
		return nil, fmt.Errorf("target ref %s does not exist; run `upstreamcheck vendor import` first", targetRef)
	}

	renameMap := make(map[string]string)
	if baseRef != "" && repo.RefExists(baseRef) {
		if diff, err := repo.DiffNameStatus(baseRef, targetRef, nil); err == nil {
			for _, d := range diff {
				if d.Status == 'R' || d.Status == 'C' {
					renameMap[d.OldPath] = d.NewPath
				}
			}
		}
	}

	targetCache := make(map[string]index.FileIndex)
	targetIndex := func(path string) (index.FileIndex, bool) {
		if cached, ok := targetCache[path]; ok {
			return cached, true
		}
		content, err := repo.ShowFile(targetRef, path)
		if err != nil {
			return index.FileIndex{}, false
		}
		idx := index.IndexFile(path, content)
		targetCache[path] = idx
		return idx, true
	}

	featureTitles := make(map[string]string)
	for _, feature := range features {
		featureTitles[feature.ID] = feature.Title
	}
	groups := make(map[string][]PlanEntry)

	for _, entry := range entries {
		plan := PlanEntry{Entry: entry}
		targetPath := entry.Path
		if renamed, ok := renameMap[targetPath]; ok {
			targetPath = renamed
		}

		switch entry.Disposition {
		case "removed":
			if !repo.FileExistsAt(targetRef, targetPath) {
				plan.Status = "done"
				plan.Detail = "gone in target"
				break
			}
			if entry.Kind == "file" {
				plan.Status = "pending"
				plan.Detail = "file still exists"
				if targetPath != entry.Path {
					plan.Detail = "renamed to " + targetPath
				}
				break
			}
			idx, ok := targetIndex(targetPath)
			if !ok {
				plan.Status = "manual"
				plan.Detail = "unreadable target file"
				break
			}
			if entry.Kind == "build_ref" || entry.Kind == "flag" || entry.Kind == "pref" || entry.Kind == "config_line" {
				lines := index.LinesSet(idx.Lines)
				if lineEntryPresent(entry.Kind, lines, entry.Symbol) {
					plan.Status = "pending"
					plan.Detail = "line still present"
				} else {
					plan.Status = "done"
					plan.Detail = "line gone"
				}
				break
			}
			if containsSymbol(idx.Symbols, entry.Symbol) {
				plan.Status = "pending"
				plan.Detail = "symbol still present"
			} else {
				plan.Status = "done"
				plan.Detail = "symbol gone"
			}
		case "added", "modified":
			plan.Status = "manual"
			plan.Detail = "behavioral or additive change; review target diff"
		case "retained":
			plan.Status = "done"
			plan.Detail = "intentionally retained"
		default:
			plan.Status = "manual"
			plan.Detail = "unknown disposition"
		}
		groups[entry.Feature] = append(groups[entry.Feature], plan)
	}

	result := &PlanResult{TargetRef: targetRef}
	for feature, plans := range groups {
		pf := PlanFeature{Feature: feature, Title: featureTitles[feature]}
		for _, plan := range plans {
			switch plan.Status {
			case "pending":
				pf.Pending++
			case "done":
				pf.Done++
			default:
				pf.Manual++
			}
			pf.Entries = append(pf.Entries, plan)
		}
		result.Features = append(result.Features, pf)
	}
	sort.SliceStable(result.Features, func(i, j int) bool {
		return result.Features[i].Feature < result.Features[j].Feature
	})
	return result, nil
}

func Refs(repo *gitx.Repo, cfg *config.Config, forkRef, feature string, limit int) ([]string, error) {
	entries, _, err := LoadManifest(cfg)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	var terms []string
	seen := make(map[string]bool)
	for _, entry := range entries {
		if feature != "" && entry.Feature != feature {
			continue
		}
		if entry.Disposition != "removed" {
			continue
		}
		term := entry.Symbol
		if entry.Kind == "file" {
			term = filepath.Base(entry.Path)
		}
		if term == "" || term == "-" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	sort.Strings(terms)

	var results []string
	for _, term := range terms {
		out, err := repo.Run("grep", "-n", "-I", "-F", "-e", term, forkRef, "--", "Source")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			results = append(results, line)
			if len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

func coveragePaths(d gitx.DiffEntry) []string {
	switch d.Status {
	case 'D':
		return []string{d.OldPath}
	case 'R', 'C':
		return []string{d.OldPath, d.NewPath}
	default:
		return []string{d.NewPath}
	}
}

func containsSymbol(symbols []index.Symbol, name string) bool {
	for _, sym := range symbols {
		if sym.Name == name {
			return true
		}
	}
	return false
}

// retainedIssue reports whether a `retained` manifest entry is missing from the
// tree under test. Retained rows are positive invariants, so they fail when the
// file, build line or symbol disappears. Returns nil when the entry is present.
func retainedIssue(entry *manifest.Entry, exists bool, idx index.FileIndex, indexed bool, ref string) *LintIssue {
	if !exists {
		return &LintIssue{
			Kind:    "retained-missing",
			Message: fmt.Sprintf("%s should be retained but is absent in %s", entry.Path, ref),
			Entry:   entry,
		}
	}
	if entry.Kind == "file" || !indexed {
		return nil
	}
	present := false
	if entry.Kind == "build_ref" || entry.Kind == "flag" || entry.Kind == "pref" || entry.Kind == "config_line" {
		present = lineEntryPresent(entry.Kind, index.LinesSet(idx.Lines), entry.Symbol)
	} else {
		present = containsSymbol(idx.Symbols, entry.Symbol)
	}
	if present {
		return nil
	}
	return &LintIssue{
		Kind:    "retained-missing",
		Message: fmt.Sprintf("%s should be retained but is absent in %s", entry.Symbol, entry.Path),
		Entry:   entry,
	}
}

// lineEntryPresent reports whether a build/config line recorded in the manifest
// is still present in the target file. Build lists such as SourcesCocoa.txt
// annotate source entries with flags (for example "@nonARC" or "@cost:3"), but
// the manifest stores the bare path. Fall back to comparing the first token so
// those entries are not reported as already removed.
func lineEntryPresent(kind string, lines map[string]index.Line, symbol string) bool {
	if _, ok := lines[symbol]; ok {
		return true
	}
	if kind != "build_ref" {
		return false
	}
	want := buildRefToken(symbol)
	if want == "" {
		return false
	}
	for text := range lines {
		if buildRefToken(text) == want {
			return true
		}
	}
	return false
}

func buildRefToken(line string) string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSuffix(fields[0], "\\")
}
