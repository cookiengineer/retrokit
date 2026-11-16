package check

import (
	"fmt"
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
				if !ok || isCommentLine(content) || ignoredLeakPath(path) {
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
	if strings.HasSuffix(p, "/news") || p == "news" {
		return true
	}
	return false
}

func isCommentLine(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "--") {
		return true
	}
	return false
}
