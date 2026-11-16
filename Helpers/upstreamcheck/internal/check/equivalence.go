package check

import (
	"fmt"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/index"
)

type EquivalenceResult struct {
	Ref      string
	Checked  int
	Errors   []LintIssue
	Warnings []LintIssue
}

// Equivalence checks a new fork tree against the removal manifest without
// requiring the new tree to match the old baseline. This is the check to run
// on a branch based on a newer upstream release.
func Equivalence(repo *gitx.Repo, cfg *config.Config, forkRef string) (*EquivalenceResult, error) {
	entries, _, err := LoadManifest(cfg)
	if err != nil {
		return nil, err
	}
	result := &EquivalenceResult{Ref: forkRef}
	cache := make(map[string]index.FileIndex)
	getIndex := func(path string) (index.FileIndex, bool) {
		if cached, ok := cache[path]; ok {
			return cached, true
		}
		content, err := repo.ShowFile(forkRef, path)
		if err != nil {
			return index.FileIndex{}, false
		}
		idx := index.IndexFile(path, content)
		cache[path] = idx
		return idx, true
	}

	for i := range entries {
		entry := &entries[i]
		if !cfg.InScope(entry.Path) {
			continue
		}
		result.Checked++
		exists := repo.FileExistsAt(forkRef, entry.Path)

		switch entry.Disposition {
		case "removed":
			if entry.Kind == "file" {
				if exists {
					result.Errors = append(result.Errors, LintIssue{
						Kind:    "removal-missing",
						Message: fmt.Sprintf("%s should be removed but exists in %s", entry.Path, forkRef),
						Entry:   entry,
					})
				}
				continue
			}
			if !exists {
				continue
			}
			if entry.Kind == "build_ref" || entry.Kind == "flag" || entry.Kind == "pref" || entry.Kind == "config_line" {
				content, err := repo.ShowFile(forkRef, entry.Path)
				if err != nil {
					continue
				}
				if _, ok := index.LinesSet(index.BuildLines(content))[entry.Symbol]; ok {
					result.Errors = append(result.Errors, LintIssue{
						Kind:    "removal-missing",
						Message: fmt.Sprintf("%s still contains removed line %q", entry.Path, entry.Symbol),
						Entry:   entry,
					})
				}
				continue
			}
			if idx, ok := getIndex(entry.Path); ok && containsSymbol(idx.Symbols, entry.Symbol) {
				result.Errors = append(result.Errors, LintIssue{
					Kind:    "removal-missing",
					Message: fmt.Sprintf("%s should be removed but exists in %s", entry.Symbol, entry.Path),
					Entry:   entry,
				})
			}

		case "added":
			if entry.Kind == "file" && !exists {
				result.Warnings = append(result.Warnings, LintIssue{
					Kind:    "addition-missing",
					Message: fmt.Sprintf("%s was recorded as added but is absent in %s", entry.Path, forkRef),
					Entry:   entry,
				})
			}

		case "modified":
			if !exists {
				result.Warnings = append(result.Warnings, LintIssue{
					Kind:    "modification-missing",
					Message: fmt.Sprintf("%s was recorded as modified but is absent in %s", entry.Path, forkRef),
					Entry:   entry,
				})
			}
		}
	}
	return result, nil
}
