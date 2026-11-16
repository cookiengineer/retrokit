package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/manifest"
)

type LeakSample struct {
	Token   string
	Path    string
	Line    int
	Content string
}

type LeakFeature struct {
	Feature string
	Title   string
	Count   int
	Samples []LeakSample
}

type LeakResult struct {
	Ref      string
	Total    int
	Features []LeakFeature
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// preprocessorRE matches C/C++ preprocessor directives. Those lines begin with
// '#' but are compiled code, not comments, so leak scans must see them.
var preprocessorRE = regexp.MustCompile(`^#\s*(?:include|include_next|import|define|undef|if|ifdef|ifndef|elif|else|endif|pragma|error|warning|line)\b`)

// preprocessorExtensions lists source types where a leading '#' can introduce a
// preprocessor directive. Files such as CMakeLists.txt, shell scripts or YAML
// use '#' only for comments and are intentionally absent.
var preprocessorExtensions = map[string]bool{
	".c":     true,
	".cc":    true,
	".cpp":   true,
	".cxx":   true,
	".h":     true,
	".hh":    true,
	".hpp":   true,
	".hxx":   true,
	".in":    true,
	".inc":   true,
	".m":     true,
	".mm":    true,
	".swift": true,
}

// Leaks scans the tracked source for feature tokens. It intentionally ignores
// comment-only lines: the goal is to find residual compiled code and build
// flags, not every mention in a changelog or comment.
func Leaks(repo *gitx.Repo, cfg *config.Config, forkRef, onlyFeature string, limit int) (*LeakResult, error) {
	features, err := manifest.LoadFeatures(cfg.ManifestPath(cfg.Manifest.Features))
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	result := &LeakResult{Ref: forkRef}

	for _, feature := range features {
		if onlyFeature != "" && feature.ID != onlyFeature {
			continue
		}
		if feature.Status == "pending" || feature.Status == "undecided" {
			// Pending features are expected to be present. Report them only
			// when explicitly requested.
			if onlyFeature == "" {
				continue
			}
		}
		lf := LeakFeature{Feature: feature.ID, Title: feature.Title}
		seen := make(map[string]bool)
		for _, token := range feature.Tokens {
			token = strings.TrimSpace(token)
			if len(token) < 3 {
				continue
			}
			args := []string{"grep", "-n", "-I", "-F"}
			if identRE.MatchString(token) {
				args = append(args, "-w")
			}
			args = append(args, "-e", token, forkRef, "--", "Source")
			out, err := repo.Run(args...)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				path, number, content, ok := parseGrepLine(line)
				if !ok || isCommentLine(path, content) || ignoredLeakPath(path) {
					continue
				}
				key := path + ":" + fmt.Sprint(number) + ":" + content
				if seen[key] {
					continue
				}
				seen[key] = true
				lf.Count++
				if len(lf.Samples) < limit {
					lf.Samples = append(lf.Samples, LeakSample{Token: token, Path: path, Line: number, Content: strings.TrimSpace(content)})
				}
			}
		}
		if lf.Count > 0 {
			result.Total += lf.Count
			result.Features = append(result.Features, lf)
		}
	}
	sort.SliceStable(result.Features, func(i, j int) bool {
		if result.Features[i].Count == result.Features[j].Count {
			return result.Features[i].Feature < result.Features[j].Feature
		}
		return result.Features[i].Count > result.Features[j].Count
	})
	return result, nil
}

func parseGrepLine(line string) (path string, number int, content string, ok bool) {
	// git grep output: ref:path:line:content
	first := strings.Index(line, ":")
	if first < 0 {
		return "", 0, "", false
	}
	rest := line[first+1:]
	second := strings.Index(rest, ":")
	if second < 0 {
		return "", 0, "", false
	}
	path = rest[:second]
	rest = rest[second+1:]
	third := strings.Index(rest, ":")
	if third < 0 {
		return "", 0, "", false
	}
	if _, err := fmt.Sscanf(rest[:third], "%d", &number); err != nil {
		return "", 0, "", false
	}
	return path, number, rest[third+1:], true
}

func ignoredLeakPath(path string) bool {
	p := strings.ToLower(path)
	if strings.Contains(p, "/thirdparty/") || strings.Contains(p, "/external/") || strings.Contains(p, ".xcodeproj/") {
		return true
	}
	// Translation catalogs are not compiled code and legitimately retain strings
	// for APIs that have since been removed from the source tree.
	if strings.Contains(p, "/po/") || strings.Contains(p, "/en.lproj/") || strings.HasSuffix(p, ".strings") {
		return true
	}
	if strings.HasSuffix(p, "/news") || p == "news" {
		return true
	}
	return false
}

func isCommentLine(path, content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "--") {
		return true
	}
	if strings.HasPrefix(trimmed, "#") {
		// Preprocessor directives and #include lines are compiled input. Only
		// treat '#' as a comment marker when the file type uses it that way.
		if preprocessorExtensions[strings.ToLower(filepath.Ext(path))] && preprocessorRE.MatchString(trimmed) {
			return false
		}
		return true
	}
	return false
}
