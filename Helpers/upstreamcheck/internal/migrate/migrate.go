package migrate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/index"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/manifest"
	"github.com/pmezard/go-difflib/difflib"
)

type Result struct {
	Feature       string
	TargetRef     string
	PatchPath     string
	ReportPath    string
	DeletedFiles  []string
	ModifiedFiles []string
	RemovedSyms   int
	RemovedLines  int
	Manual        []string
	Missing       []string
	NewTargets    []string
}

type edit struct {
	start int
	end   int
	kind  string
}

type fileEdits struct {
	path      string
	original  []byte
	edits     []edit
	deleteAll bool
	removed   []string
}

// Generate writes a candidate patch for one feature. It never touches the
// working tree.
func Generate(repo *gitx.Repo, cfg *config.Config, baseRef, targetRef, feature, outDir string) (*Result, error) {
	entries, err := manifest.LoadEntries(cfg.ManifestPath(cfg.Manifest.Divergence))
	if err != nil {
		return nil, err
	}
	features, err := manifest.LoadFeatures(cfg.ManifestPath(cfg.Manifest.Features))
	if err != nil {
		return nil, err
	}
	if !repo.RefExists(targetRef) {
		return nil, fmt.Errorf("target ref %s does not exist", targetRef)
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

	files := make(map[string]*fileEdits)
	result := &Result{Feature: feature, TargetRef: targetRef}

	getFile := func(path string) (*fileEdits, error) {
		if f, ok := files[path]; ok {
			return f, nil
		}
		content, err := repo.ShowFile(targetRef, path)
		if err != nil {
			return nil, err
		}
		f := &fileEdits{path: path, original: content}
		files[path] = f
		return f, nil
	}

	for _, entry := range entries {
		if entry.Feature != feature {
			continue
		}
		targetPath := entry.Path
		if renamed, ok := renameMap[targetPath]; ok {
			targetPath = renamed
		}

		switch entry.Disposition {
		case "removed":
			if !repo.FileExistsAt(targetRef, targetPath) {
				result.Missing = append(result.Missing, entry.Path+" (already gone)")
				continue
			}
			switch entry.Kind {
			case "file":
				f, err := getFile(targetPath)
				if err != nil {
					result.Missing = append(result.Missing, entry.Path+" (unreadable)")
					continue
				}
				f.deleteAll = true
				result.DeletedFiles = append(result.DeletedFiles, targetPath)
			case "build_ref", "flag":
				f, err := getFile(targetPath)
				if err != nil {
					result.Missing = append(result.Missing, entry.Path+" (unreadable)")
					continue
				}
				if start, end, ok := findLine(f.original, entry.Symbol); ok {
					f.edits = append(f.edits, edit{start: start, end: end, kind: "line"})
					f.removed = append(f.removed, entry.Symbol)
					result.RemovedLines++
				} else {
					result.Missing = append(result.Missing, entry.Symbol+" ("+entry.Path+")")
				}
			case "pref", "config_line", "behavior":
				result.Manual = append(result.Manual, fmt.Sprintf("%s %s in %s (%s)", entry.Kind, entry.Symbol, entry.Path, entry.Origin))
			default:
				f, err := getFile(targetPath)
				if err != nil {
					result.Missing = append(result.Missing, entry.Path+" (unreadable)")
					continue
				}
				idx := index.IndexFile(targetPath, f.original)
				spans := findSymbols(idx.Symbols, entry.Symbol)
				if len(spans) == 0 {
					result.Missing = append(result.Missing, entry.Symbol+" ("+entry.Path+")")
					continue
				}
				for _, sym := range spans {
					start, end := expandSpan(f.original, sym)
					f.edits = append(f.edits, edit{start: start, end: end, kind: sym.Kind})
					result.RemovedSyms++
				}
				f.removed = append(f.removed, entry.Symbol)
			}
		case "added", "modified":
			result.Manual = append(result.Manual, fmt.Sprintf("%s %s in %s (%s)", entry.Kind, entry.Symbol, entry.Path, entry.Origin))
		default:
			result.Manual = append(result.Manual, fmt.Sprintf("%s %s in %s", entry.Kind, entry.Symbol, entry.Path))
		}
	}

	var diffBuf strings.Builder
	for path, f := range files {
		if f.deleteAll {
			diffBuf.WriteString(deletePatch(path, f.original))
			continue
		}
		if len(f.edits) == 0 {
			continue
		}
		modified := applyEdits(f.original, f.edits)
		if bytes.Equal(modified, f.original) {
			continue
		}
		patch, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(f.original)),
			B:        difflib.SplitLines(string(modified)),
			FromFile: "a/" + path,
			ToFile:   "b/" + path,
			Context:  3,
		})
		if err != nil {
			return nil, err
		}
		diffBuf.WriteString(patch)
		result.ModifiedFiles = append(result.ModifiedFiles, path)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	result.PatchPath = filepath.Join(outDir, feature+".patch")
	if err := os.WriteFile(result.PatchPath, []byte(diffBuf.String()), 0o644); err != nil {
		return nil, err
	}

	result.NewTargets = findNewTargets(repo, cfg, targetRef, feature, features, entries)
	sort.Strings(result.DeletedFiles)
	sort.Strings(result.ModifiedFiles)
	sort.Strings(result.Manual)
	sort.Strings(result.Missing)
	sort.Strings(result.NewTargets)

	result.ReportPath = filepath.Join(outDir, feature+".md")
	if err := os.WriteFile(result.ReportPath, []byte(renderReport(result)), 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

func findSymbols(symbols []index.Symbol, name string) []index.Symbol {
	var result []index.Symbol
	for _, sym := range symbols {
		if sym.Name == name {
			result = append(result, sym)
		}
	}
	return result
}

func expandSpan(content []byte, sym index.Symbol) (int, int) {
	start, end := sym.Start, sym.End
	for start > 0 && (content[start-1] == ' ' || content[start-1] == '\t') {
		start--
	}
	if sym.Kind == "enum_value" {
		for end < len(content) && (content[end] == ' ' || content[end] == '\t') {
			end++
		}
		if end < len(content) && content[end] == ',' {
			end++
			for end < len(content) && (content[end] == ' ' || content[end] == '\t') {
				end++
			}
		}
	}
	if end < len(content) && content[end] == '\r' {
		end++
	}
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return start, end
}

func applyEdits(content []byte, edits []edit) []byte {
	if len(edits) == 0 {
		return content
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end > edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var filtered []edit
	for _, e := range edits {
		if e.start < 0 || e.end > len(content) || e.start >= e.end {
			continue
		}
		if len(filtered) > 0 && e.start < filtered[len(filtered)-1].end {
			// Overlapping declarations (for example a class and one of its
			// members). Keep the larger span.
			last := &filtered[len(filtered)-1]
			if e.end > last.end {
				last.end = e.end
			}
			continue
		}
		filtered = append(filtered, e)
	}
	var out bytes.Buffer
	prev := 0
	for _, e := range filtered {
		out.Write(content[prev:e.start])
		prev = e.end
	}
	out.Write(content[prev:])
	return out.Bytes()
}

func findLine(content []byte, text string) (int, int, bool) {
	lines := strings.Split(string(content), "\n")
	offset := 0
	for _, line := range lines {
		lineLen := len(line)
		if strings.TrimSpace(line) == text {
			start := offset
			end := offset + lineLen
			if end < len(content) && content[end] == '\r' {
				end++
			}
			if end < len(content) && content[end] == '\n' {
				end++
			}
			return start, end, true
		}
		offset += lineLen + 1
	}
	return 0, 0, false
}

func deletePatch(path string, content []byte) string {
	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	b.WriteString("deleted file mode 100644\n")
	fmt.Fprintf(&b, "--- a/%s\n", path)
	b.WriteString("+++ /dev/null\n")
	fmt.Fprintf(&b, "@@ -1,%d +0,0 @@\n", len(lines))
	for _, line := range lines {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func renderReport(result *Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Migration candidate: %s\n\n", result.Feature)
	fmt.Fprintf(&b, "Target: `%s`\n\n", result.TargetRef)
	fmt.Fprintf(&b, "- Deleted files: %d\n", len(result.DeletedFiles))
	fmt.Fprintf(&b, "- Modified files: %d\n", len(result.ModifiedFiles))
	fmt.Fprintf(&b, "- Removed symbols: %d\n", result.RemovedSyms)
	fmt.Fprintf(&b, "- Removed build/config lines: %d\n", result.RemovedLines)
	fmt.Fprintf(&b, "- Manual entries: %d\n", len(result.Manual))
	fmt.Fprintf(&b, "- Symbols not found in target: %d\n", len(result.Missing))
	fmt.Fprintf(&b, "- Possible new target files: %d\n\n", len(result.NewTargets))

	section := func(title string, values []string) {
		if len(values) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		for _, value := range values {
			fmt.Fprintf(&b, "- `%s`\n", value)
		}
		b.WriteByte('\n')
	}
	section("Deleted files", result.DeletedFiles)
	section("Modified files", result.ModifiedFiles)
	section("Manual review", result.Manual)
	section("Not found in target", result.Missing)
	section("Possible new upstream files", result.NewTargets)
	return b.String()
}

func findNewTargets(repo *gitx.Repo, cfg *config.Config, targetRef, feature string, features []manifest.Feature, entries []manifest.Entry) []string {
	var tokens []string
	for _, f := range features {
		if f.ID == feature {
			tokens = f.Tokens
			break
		}
	}

	known := make(map[string]bool)
	featureDirs := make(map[string]bool)
	for _, entry := range entries {
		if entry.Kind != "file" {
			continue
		}
		known[entry.Path] = true
		if entry.Feature != feature || entry.Disposition != "removed" {
			continue
		}
		dir := filepath.Dir(entry.Path)
		if override, _, ok := pathFeatureOverrideForMigrate(dir + "/"); ok && override == feature {
			featureDirs[dir] = true
		}
	}
	if len(tokens) == 0 && len(featureDirs) == 0 {
		return nil
	}

	targetPaths, err := repo.TreePaths(targetRef, cfg.Vendor.Paths)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for path := range targetPaths {
		if known[path] || seen[path] {
			continue
		}
		for dir := range featureDirs {
			if strings.HasPrefix(path, dir+"/") {
				result = append(result, path)
				seen[path] = true
				break
			}
		}
		if seen[path] {
			continue
		}
		base := strings.ToLower(filepath.Base(path))
		for _, token := range tokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if len(token) < 3 {
				continue
			}
			if strings.Contains(base, token) {
				result = append(result, path)
				seen[path] = true
				break
			}
		}
	}
	return result
}

// pathFeatureOverrideForMigrate mirrors the path-based attribution used by the
// manifest generator. Directories are passed with a trailing slash so paths
// such as platform/xr/ are recognised.
func pathFeatureOverrideForMigrate(path string) (string, string, bool) {
	p := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	switch {
	case strings.Contains(p, "webxr") || strings.Contains(p, "/xr/") || strings.Contains(p, "openxr") || strings.Contains(p, "fakexr") || strings.Contains(p, "webgpuxr"):
		return "webxr", "WebXR API", true
	case strings.Contains(p, "gamepad"):
		return "gamepad", "Gamepad API", true
	case strings.Contains(p, "geolocation"):
		return "geolocation", "Geolocation API", true
	case strings.Contains(p, "mediastream") || strings.Contains(p, "getusermedia") || strings.Contains(p, "mediadevices"):
		return "media-capture", "Media Capture and Streams API", true
	case strings.Contains(p, "encryptedmedia"):
		return "encrypted-media", "Encrypted Media API", true
	case strings.Contains(p, "paymentrequest") || strings.Contains(p, "applepay"):
		return "payment", "Payment and Apple Pay APIs", true
	case strings.Contains(p, "graphicscontextgl") || strings.Contains(p, "/canvas/webgl") || strings.Contains(p, "thirdparty/angle") || strings.Contains(p, "opengl"):
		return "webgl", "WebGL API", true
	case strings.Contains(p, "webrtc"):
		return "webrtc", "WebRTC API", true
	case strings.Contains(p, "webdriver") || strings.Contains(p, "automation"):
		return "automation", "Web Automation and WebDriver", true
	case strings.Contains(p, "fullscreen") || strings.Contains(p, "pictureinpicture") || strings.Contains(p, "picture-in-picture"):
		return "fullscreen-pip", "Fullscreen and Picture-in-Picture", true
	case strings.Contains(p, "devicemotion") || strings.Contains(p, "deviceorientation"):
		return "device-sensors", "Device Motion and Orientation APIs", true
	case strings.Contains(p, "wirelessplayback") || strings.Contains(p, "remoteplayback"):
		return "wireless-playback", "Wireless Playback Target API", true
	case strings.Contains(p, "plugin"):
		return "plugins", "Legacy Plugin APIs", true
	}
	return "", "", false
}
