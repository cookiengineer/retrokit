package bootstrap

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/index"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/manifest"
)

type ReviewItem struct {
	Commit  string
	Subject string
	Paths   []string
}

type Result struct {
	Entries     []manifest.Entry
	OutOfScope  []manifest.Entry
	Review      []ReviewItem
	Features    []manifest.Feature
	CommitCount int
}

type pathInfo struct {
	feature     string
	title       string
	lastOrigin  string
	lastSubject string
	origins     []string
}

var emojiPrefix = regexp.MustCompile(`^:[a-z0-9_+-]+:\s*`)
var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Run derives the divergence manifest from the fork history and the net diff
// between the upstream baseline and the fork.
func Run(repo *gitx.Repo, cfg *config.Config, baseRef, forkRef string) (*Result, error) {
	commits, err := repo.ListCommits(baseRef, forkRef)
	if err != nil {
		return nil, err
	}

	paths := make(map[string]*pathInfo)
	var review []ReviewItem
	var historyEntries []manifest.Entry

	for _, commit := range commits {
		feature, title := featureForSubject(commit.Subject)
		diff, err := repo.DiffTree(commit.Hash, nil)
		if err != nil {
			return nil, fmt.Errorf("diff %s: %w", commit.Hash, err)
		}
		if feature == "maintenance" || feature == "misc" || feature == "docs" {
			// Fix commits inherit the feature of the files they touch. Keep a
			// review note for paths that do not belong to a known feature yet.
			var unknown []string
			for _, entry := range diff {
				for _, p := range diffPaths(entry) {
					if info := paths[p]; info != nil && info.feature != "maintenance" && info.feature != "misc" {
						info.origins = append(info.origins, commit.Hash)
						info.lastOrigin = commit.Hash
						info.lastSubject = commit.Subject
						continue
					}
					if feature == "maintenance" && cfg.InScope(p) && isSourcePath(p) {
						unknown = append(unknown, p)
					}
				}
			}
			if len(unknown) > 0 {
				sort.Strings(unknown)
				review = append(review, ReviewItem{Commit: commit.Hash, Subject: commit.Subject, Paths: unique(unknown)})
			}
		}

		for _, entry := range diff {
			if (entry.Status == 'M' || entry.Status == 'R' || entry.Status == 'C') &&
				len(commit.Parents) > 0 && cfg.InScope(entry.NewPath) && index.IsBuild(entry.NewPath) {
				parent := commit.Parents[0]
				parentContent, parentErr := repo.ShowFile(parent, entry.OldPath)
				commitContent, commitErr := repo.ShowFile(commit.Hash, entry.NewPath)
				if parentErr == nil && commitErr == nil {
					removedLines, _ := index.DiffLines(index.BuildLines(parentContent), index.BuildLines(commitContent))
					for i, line := range removedLines {
						if i >= 300 {
							break
						}
						if !usefulBuildLine(entry.NewPath, line.Text) {
							continue
						}
						lineFeature := feature
						if override, _, ok := pathFeatureOverride(line.Text); ok {
							lineFeature = override
						}
						historyEntries = append(historyEntries, manifest.Entry{
							Feature: lineFeature, Kind: buildKind(entry.NewPath, line.Text),
							Symbol: truncate(line.Text, 300), Path: entry.NewPath,
							Disposition: "removed", Scope: cfg.ScopeFor(entry.NewPath),
							Origin: commit.Hash,
							Notes:  fmt.Sprintf("line=%d; commit=%s", line.Number, shortHash(commit.Hash)),
						})
					}
				}
			}

			for _, p := range diffPaths(entry) {
				info := paths[p]
				if info == nil {
					info = &pathInfo{feature: feature, title: title}
					paths[p] = info
				}
				if specialFeature, specialTitle, ok := specialFeatureForPath(p); ok {
					info.feature = specialFeature
					info.title = specialTitle
				} else if isTranslationPath(p) {
					info.feature = "translations"
					info.title = "Translation Catalogs"
				} else if feature != "maintenance" && feature != "misc" && feature != "docs" {
					info.feature = feature
					info.title = title
				}
				info.origins = append(info.origins, commit.Hash)
				info.lastOrigin = commit.Hash
				info.lastSubject = commit.Subject
			}
		}
	}

	// Commits often bundle multiple removals (for example, the Gamepad
	// commit deleted the WebXR modules). Reassign paths with an unmistakable
	// feature name in their path so each feature can be migrated on its own.
	for path, info := range paths {
		if specialFeature, specialTitle, ok := specialFeatureForPath(path); ok {
			info.feature = specialFeature
			info.title = specialTitle
			continue
		}
		if feature, title, ok := pathFeatureOverride(path); ok {
			info.feature = feature
			info.title = title
		}
	}

	net, err := repo.DiffNameStatus(baseRef, forkRef, nil)
	if err != nil {
		return nil, err
	}

	result := &Result{CommitCount: len(commits)}
	featureTitles := make(map[string]string)

	for _, diff := range net {
		oldPath := diff.OldPath
		newPath := diff.NewPath
		if diff.Status == 'D' {
			newPath = oldPath
		}
		if diff.Status == 'A' {
			oldPath = newPath
		}
		path := newPath
		info := paths[path]
		if info == nil {
			info = paths[oldPath]
		}
		if info == nil {
			info = &pathInfo{feature: "misc", title: "Miscellaneous divergence"}
		}
		feature := info.feature
		if specialFeature, specialTitle, ok := specialFeatureForPath(path); ok {
			feature = specialFeature
			info.title = specialTitle
		} else if isTranslationPath(path) {
			feature = "translations"
			info.title = "Translation Catalogs"
		} else if override, overrideTitle, ok := pathFeatureOverride(path); ok {
			feature = override
			info.title = overrideTitle
		}
		if feature == "" {
			feature = "misc"
		}
		if featureTitles[feature] == "" {
			featureTitles[feature] = info.title
		}

		scope := cfg.ScopeFor(path)
		inScope := cfg.InScope(path)
		origin := info.lastOrigin
		notes := noteFor(info, diff)

		entry := manifest.Entry{
			Feature:     feature,
			Kind:        "file",
			Symbol:      "-",
			Path:        path,
			Disposition: dispositionFor(diff.Status),
			Scope:       scope,
			Origin:      origin,
			Notes:       notes,
		}
		if inScope {
			result.Entries = append(result.Entries, entry)
		} else {
			result.OutOfScope = append(result.OutOfScope, entry)
		}

		// Git's rename detection depends on the pathspec and the surrounding
		// tree. Record both sides of a rename so coverage checks are stable
		// even when a later diff reports the same change as delete+add.
		if (diff.Status == 'R' || diff.Status == 'C') && oldPath != newPath {
			relDisposition := "renamed"
			if diff.Status == 'C' {
				relDisposition = "copied"
			}
			relEntry := manifest.Entry{
				Feature:     feature,
				Kind:        "file",
				Symbol:      "-",
				Path:        oldPath,
				Disposition: relDisposition,
				Scope:       cfg.ScopeFor(oldPath),
				Origin:      origin,
				Notes:       "renamed to " + newPath,
			}
			if cfg.InScope(oldPath) {
				result.Entries = append(result.Entries, relEntry)
			} else {
				result.OutOfScope = append(result.OutOfScope, relEntry)
			}
		}

		if diff.Status != 'M' && diff.Status != 'R' {
			continue
		}

		basePath := oldPath
		forkPath := newPath
		if !cfg.InScope(forkPath) {
			continue
		}

		if index.IsCpp(forkPath) || index.IsIDL(forkPath) {
			baseContent, err := repo.ShowFile(baseRef, basePath)
			if err != nil {
				continue
			}
			forkContent, err := repo.ShowFile(forkRef, forkPath)
			if err != nil {
				continue
			}
			baseIndex := index.IndexFile(basePath, baseContent)
			forkIndex := index.IndexFile(forkPath, forkContent)
			removed, added := diffSymbols(baseIndex.Symbols, forkIndex.Symbols)
			for _, sym := range removed {
				result.Entries = append(result.Entries, manifest.Entry{
					Feature: feature, Kind: sym.Kind, Symbol: sym.Name, Path: forkPath,
					Disposition: "removed", Scope: scope, Origin: origin,
					Notes: fmt.Sprintf("line=%d", sym.StartLine),
				})
			}
			for _, sym := range added {
				result.Entries = append(result.Entries, manifest.Entry{
					Feature: feature, Kind: sym.Kind, Symbol: sym.Name, Path: forkPath,
					Disposition: "added", Scope: scope, Origin: origin,
					Notes: fmt.Sprintf("line=%d", sym.StartLine),
				})
			}
			continue
		}

		if index.IsBuild(forkPath) {
			baseContent, err := repo.ShowFile(baseRef, basePath)
			if err != nil {
				continue
			}
			forkContent, err := repo.ShowFile(forkRef, forkPath)
			if err != nil {
				continue
			}
			baseLines := index.BuildLines(baseContent)
			forkLines := index.BuildLines(forkContent)
			removedLines, addedLines := index.DiffLines(baseLines, forkLines)
			for i, line := range removedLines {
				if i >= 300 {
					break
				}
				if !usefulBuildLine(forkPath, line.Text) {
					continue
				}
				result.Entries = append(result.Entries, manifest.Entry{
					Feature: feature, Kind: buildKind(forkPath, line.Text), Symbol: truncate(line.Text, 300),
					Path: forkPath, Disposition: "removed", Scope: scope, Origin: origin,
					Notes: fmt.Sprintf("line=%d", line.Number),
				})
			}
			for i, line := range addedLines {
				if i >= 300 {
					break
				}
				if !usefulBuildLine(forkPath, line.Text) {
					continue
				}
				result.Entries = append(result.Entries, manifest.Entry{
					Feature: feature, Kind: buildKind(forkPath, line.Text), Symbol: truncate(line.Text, 300),
					Path: forkPath, Disposition: "added", Scope: scope, Origin: origin,
					Notes: fmt.Sprintf("line=%d", line.Number),
				})
			}
		}
	}

	result.Entries = append(result.Entries, historyEntries...)
	result.Entries = manifest.Dedup(result.Entries)
	result.OutOfScope = manifest.Dedup(result.OutOfScope)
	result.Review = dedupReview(review)
	result.Features = buildFeatures(result.Entries, featureTitles)
	return result, nil
}

func diffPaths(entry gitx.DiffEntry) []string {
	if entry.Status == 'R' || entry.Status == 'C' {
		return []string{entry.OldPath, entry.NewPath}
	}
	return []string{entry.NewPath}
}

func dispositionFor(status byte) string {
	switch status {
	case 'A', 'C':
		return "added"
	case 'D':
		return "removed"
	default:
		return "modified"
	}
}

func noteFor(info *pathInfo, diff gitx.DiffEntry) string {
	parts := []string{}
	if info != nil && info.lastSubject != "" {
		parts = append(parts, "origin="+info.lastOrigin+" "+info.lastSubject)
	}
	if diff.Status == 'R' {
		parts = append(parts, "renamed from "+diff.OldPath)
	}
	return strings.Join(parts, "; ")
}

func isSourcePath(path string) bool {
	return strings.HasPrefix(path, "Source/") && (index.IsCpp(path) || index.IsIDL(path))
}

func isTranslationPath(path string) bool {
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.HasSuffix(path, ".po") || strings.Contains(path, "/platform/gtk/po/")
}

// pathFeatureOverride assigns paths that contain an unambiguous feature name
// to that feature. This corrects bundled commits, where one commit removed
// files belonging to several features.
func pathFeatureOverride(path string) (string, string, bool) {
	p := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	switch {
	case strings.Contains(p, "webxr") || strings.Contains(p, "/xr/") || strings.Contains(p, "openxr") || strings.Contains(p, "fakexr"):
		return "webxr", "WebXR API", true
	case strings.Contains(p, "gamepad"):
		return "gamepad", "Gamepad API", true
	case strings.Contains(p, "geolocation"):
		return "geolocation", "Geolocation API", true
	case strings.Contains(p, "mediastream") || strings.Contains(p, "getusermedia") || strings.Contains(p, "mediadevices"):
		return "media-capture", "Media Capture and Streams API", true
	case strings.Contains(p, "encryptedmedia"):
		return "encrypted-media", "Encrypted Media API", true
	case strings.Contains(p, "mediarecorder"):
		return "media-recorder", "Media Recorder API", true
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
	case strings.Contains(p, "mediausage"):
		return "media-usage", "Media Usage API", true
	case strings.Contains(p, "mediastatistics"):
		return "media-statistics", "Media Statistics API", true
	case strings.Contains(p, "plugin"):
		return "plugins", "Legacy Plugin APIs", true
	case strings.Contains(p, "incrementalpdf"):
		return "incremental-pdf", "Incremental PDF APIs", true
	case strings.Contains(p, "quicklook"):
		return "quick-look", "Quick Look Integration", true
	case strings.Contains(p, "touchbar"):
		return "touch-bar", "Touch Bar Support", true
	}
	return "", "", false
}

func specialFeatureForPath(path string) (string, string, bool) {
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(path, "Helpers/") {
		return "tooling", "Maintenance Tooling", true
	}
	switch path {
	case "README.md", "SECURITY.md", "TODO.md", "NOTES.md":
		return "docs", "Documentation", true
	case ".gitignore", "build.sh", "clean.sh", "search.sh":
		return "tooling", "Maintenance Tooling", true
	}
	return "", "", false
}

func diffSymbols(base, fork []index.Symbol) (removed, added []index.Symbol) {
	baseSet := index.SymbolSet(base)
	forkSet := index.SymbolSet(fork)

	// Diff by name, not kind: upstream often changes a declaration from a
	// free function to a method without changing the actual API name.
	baseByName := make(map[string]index.Symbol, len(baseSet))
	for _, sym := range baseSet {
		baseByName[sym.Name] = sym
	}
	forkByName := make(map[string]index.Symbol, len(forkSet))
	for _, sym := range forkSet {
		forkByName[sym.Name] = sym
	}

	for name, sym := range baseByName {
		if _, ok := forkByName[name]; !ok {
			removed = append(removed, sym)
		}
	}
	for name, sym := range forkByName {
		if _, ok := baseByName[name]; !ok {
			added = append(added, sym)
		}
	}
	removed = collapseContainers(removed)
	added = collapseContainers(added)
	index.SortSymbols(removed)
	index.SortSymbols(added)
	return removed, added
}

func collapseContainers(symbols []index.Symbol) []index.Symbol {
	containers := make(map[string]bool)
	for _, sym := range symbols {
		switch sym.Kind {
		case "class", "struct", "union", "namespace", "interface", "dictionary", "enum":
			containers[sym.Name] = true
		}
	}
	if len(containers) == 0 {
		return symbols
	}
	var result []index.Symbol
	for _, sym := range symbols {
		skip := false
		for container := range containers {
			if sym.Name != container && strings.HasPrefix(sym.Name, container+"::") {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, sym)
		}
	}
	return result
}

func usefulBuildLine(path, text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	switch t {
	case "endif ()", "else", "}", "{", ")", "(", "WebCore:", "WebKit:", "WebKitLegacy:", "defaultValue:", "type: bool", "type: string", "default: false", "default: true":
		return false
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		if strings.HasSuffix(t, ":") && !strings.Contains(t, " ") {
			return true
		}
		for _, prefix := range []string{"humanReadableName:", "humanReadableDescription:", "condition:", "default: WebKit::", "default: WebCore::", "default: PAL::", "status:", "webPreference:"} {
			if strings.HasPrefix(t, prefix) {
				return true
			}
		}
		return false
	}
	if strings.HasSuffix(lower, ".json") {
		_, _, ok := pathFeatureOverride(t)
		return ok
	}
	if strings.HasPrefix(t, "list(APPEND ") {
		_, _, ok := pathFeatureOverride(t)
		return ok
	}
	return true
}

func buildKind(path, text string) string {
	if strings.Contains(text, "ENABLE_") || strings.Contains(text, "USE_") ||
		strings.Contains(text, "WEBKIT_") || strings.Contains(text, "DEFAULT") {
		return "flag"
	}
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if strings.Contains(text, ":") {
			return "pref"
		}
	}
	if strings.Contains(text, "/") || strings.HasSuffix(text, ".cpp") || strings.HasSuffix(text, ".h") ||
		strings.HasSuffix(text, ".idl") || strings.HasSuffix(text, ".mm") || strings.HasSuffix(text, ".c") {
		return "build_ref"
	}
	return "config_line"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func unique(values []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func dedupReview(items []ReviewItem) []ReviewItem {
	seen := make(map[string]bool)
	var result []ReviewItem
	for _, item := range items {
		if seen[item.Commit] {
			continue
		}
		seen[item.Commit] = true
		result = append(result, item)
	}
	return result
}

func buildFeatures(entries []manifest.Entry, titles map[string]string) []manifest.Feature {
	defaults := defaultFeatureMap()
	seen := make(map[string]bool)
	var features []manifest.Feature
	for _, entry := range entries {
		if seen[entry.Feature] {
			continue
		}
		seen[entry.Feature] = true
		if feature, ok := defaults[entry.Feature]; ok {
			features = append(features, feature)
			continue
		}
		title := titles[entry.Feature]
		if title == "" {
			title = entry.Feature
		}
		status := "removed"
		if entry.Feature == "maintenance" || entry.Feature == "misc" || entry.Feature == "docs" {
			status = "maintenance"
		}
		features = append(features, manifest.Feature{ID: entry.Feature, Title: title, Status: status})
	}
	for _, feature := range defaults {
		if !seen[feature.ID] {
			features = append(features, feature)
		}
	}
	sort.SliceStable(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	return features
}

func defaultFeatureMap() map[string]manifest.Feature {
	values := []manifest.Feature{
		{ID: "webgl", Title: "WebGL API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEBGL", "WebGL", "GraphicsContextGL"}},
		{ID: "webgl2", Title: "WebGL2 API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEBGL2", "WebGL2"}},
		{ID: "webrtc", Title: "WebRTC API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEB_RTC", "WebRTC", "RTCPeerConnection", "RTCDataChannel"}},
		{ID: "media-capture", Title: "Media Capture and Streams API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"MEDIA_CAPTURE", "MEDIA_STREAM", "getUserMedia", "MediaStream", "MediaDevices"}},
		{ID: "media-recorder", Title: "Media Recorder API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"MediaRecorder", "MEDIA_RECORDER"}},
		{ID: "encrypted-media", Title: "Encrypted Media API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"ENCRYPTED_MEDIA", "EME", "MediaKeys"}},
		{ID: "gamepad", Title: "Gamepad API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"GAMEPAD", "Gamepad", "navigator.getGamepads"}},
		{ID: "geolocation", Title: "Geolocation API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"GEOLOCATION", "Geolocation", "Geoposition"}},
		{ID: "device-sensors", Title: "Device Motion and Orientation APIs", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"DEVICE_ORIENTATION", "DeviceMotion", "DeviceOrientation"}},
		{ID: "webxr", Title: "WebXR API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEBXR", "WebXR", "XRSession"}},
		{ID: "payment", Title: "Payment and Apple Pay APIs", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"PAYMENT_REQUEST", "APPLE_PAY", "ApplePay", "PaymentRequest"}},
		{ID: "automation", Title: "Web Automation and WebDriver", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"WEBDRIVER", "WebAutomation", "AutomationSession"}},
		{ID: "wireless-playback", Title: "Wireless Playback Target API", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"WIRELESS_PLAYBACK_TARGET", "RemotePlayback", "PlaybackTarget"}},
		{ID: "fullscreen-pip", Title: "Fullscreen and Picture-in-Picture", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"FULLSCREEN", "PICTURE_IN_PICTURE", "EnterPictureInPictureEvent"}},
		{ID: "plugins", Title: "Legacy Plugin APIs", Status: "removed", SecurityRef: "SECURITY.md#legacy-plugins", Tokens: []string{"NETSCAPE_PLUGIN_API", "NPAPI", "PluginDocument", "RenderEmbeddedObject"}},
		{ID: "quirks", Title: "Site-specific Quirks", Status: "removed", SecurityRef: "SECURITY.md#legacy-quirks", Tokens: []string{"needsSiteSpecificQuirks", "shouldAutoplayForArbitraryUserGesture"}},
		{ID: "libsecret", Title: "libsecret Integration", Status: "removed", SecurityRef: "SECURITY.md#library-dependencies", Tokens: []string{"LIBSECRET", "libsecret"}},
		{ID: "libnotify", Title: "libnotify Integration", Status: "removed", SecurityRef: "SECURITY.md#library-dependencies", Tokens: []string{"LIBNOTIFY", "libnotify"}},
		{ID: "systemd", Title: "SystemD Integration", Status: "removed", SecurityRef: "SECURITY.md#library-dependencies", Tokens: []string{"USE_SYSTEMD", "JOURNALD", "libsystemd"}},
		{ID: "translations", Title: "Translation Catalogs", Status: "removed", SecurityRef: "SECURITY.md#legacy-quirks", Tokens: []string{}},
		{ID: "arkit-model", Title: "ARKIT and Model Element Support", Status: "removed", SecurityRef: "SECURITY.md#library-dependencies", Tokens: []string{"ARKIT", "MODEL_ELEMENT", "HTMLModelElement"}},
		{ID: "custom-protocol", Title: "Legacy Custom Protocol Support", Status: "removed", SecurityRef: "SECURITY.md#legacy-hacks", Tokens: []string{"LEGACY_CUSTOM_PROTOCOL_MANAGER", "LegacyCustomProtocol"}},
		{ID: "ftp", Title: "FTP Support", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"FTPDIR", "FTPDirectory"}},
		{ID: "incremental-pdf", Title: "Incremental PDF APIs", Status: "removed", SecurityRef: "SECURITY.md#legacy-plugins", Tokens: []string{"INCREMENTAL_PDF_APIS", "IncrementalPDF"}},
		{ID: "media-statistics", Title: "Media Statistics API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"MEDIA_STATISTICS", "webkitDecodedFrameCount"}},
		{ID: "media-usage", Title: "Media Usage API", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"MEDIA_USAGE", "MediaUsageManager"}},
		{ID: "platform-hurd", Title: "GNU/HURD Platform Support", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"HURD", "__GNU__"}},
		{ID: "platform-playstation", Title: "PlayStation Platform Support", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"PLATFORM_PLAYSTATION", "PlayStation"}},
		{ID: "platform-unix", Title: "Outdated Unix Platform Support", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"__sun", "HPUX", "IRIX", "__osf"}},
		{ID: "platform-watchos", Title: "watchOS Platform Support", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"TARGET_OS_WATCH", "WATCHOS"}},
		{ID: "quick-look", Title: "Quick Look Integration", Status: "removed", SecurityRef: "SECURITY.md#library-dependencies", Tokens: []string{"QUICK_LOOK", "ARKIT_QUICK_LOOK_PREVIEW"}},
		{ID: "sleep-disablers", Title: "Sleep Disablers", Status: "removed", SecurityRef: "SECURITY.md#legacy-hacks", Tokens: []string{"SleepDisabler", "shouldDisableSleep"}},
		{ID: "touch-bar", Title: "Touch Bar Support", Status: "removed", SecurityRef: "SECURITY.md#target-platforms", Tokens: []string{"TOUCH_BAR", "TouchBarMenu"}},
		{ID: "translation-ui", Title: "Translation UI Services", Status: "removed", SecurityRef: "SECURITY.md#legacy-plugins", Tokens: []string{"TRANSLATION_UI_SERVICES"}},
		{ID: "web-api-statistics", Title: "Web API Statistics", Status: "removed", SecurityRef: "SECURITY.md#ecmascript-apis", Tokens: []string{"WEB_API_STATISTICS"}},
		{ID: "maintenance", Title: "Maintenance and build fixes", Status: "maintenance", Tokens: []string{}},
		{ID: "misc", Title: "Miscellaneous divergence", Status: "maintenance", Tokens: []string{}},
		{ID: "docs", Title: "Documentation", Status: "maintenance", Tokens: []string{}},
		{ID: "tooling", Title: "Maintenance Tooling", Status: "maintenance", Tokens: []string{}},
	}
	result := make(map[string]manifest.Feature, len(values))
	for _, feature := range values {
		result[feature.ID] = feature
	}
	return result
}

func featureForSubject(subject string) (string, string) {
	clean := strings.TrimSpace(emojiPrefix.ReplaceAllString(subject, ""))
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "remove ") {
		lower = strings.TrimSpace(strings.TrimPrefix(lower, "remove "))
	}
	if strings.HasPrefix(lower, "removed ") {
		lower = strings.TrimSpace(strings.TrimPrefix(lower, "removed "))
	}
	title := clean
	if strings.HasPrefix(strings.ToLower(title), "remove ") {
		title = strings.TrimSpace(title[len("remove "):])
	}
	if strings.HasPrefix(strings.ToLower(title), "removed ") {
		title = strings.TrimSpace(title[len("removed "):])
	}
	clean = title

	switch {
	case strings.Contains(lower, "webgl"):
		return "webgl", clean
	case strings.Contains(lower, "web rtc") || strings.Contains(lower, "webrtc"):
		return "webrtc", clean
	case strings.Contains(lower, "media capture") || strings.Contains(lower, "media stream"):
		return "media-capture", clean
	case strings.Contains(lower, "media recorder"):
		return "media-recorder", clean
	case strings.Contains(lower, "media usage"):
		return "media-usage", clean
	case strings.Contains(lower, "media statistics"):
		return "media-statistics", clean
	case strings.Contains(lower, "encrypted media"):
		return "encrypted-media", clean
	case strings.Contains(lower, "gamepad"):
		return "gamepad", clean
	case strings.Contains(lower, "geolocation"):
		return "geolocation", clean
	case strings.Contains(lower, "device motion") || strings.Contains(lower, "device orientation"):
		return "device-sensors", clean
	case strings.Contains(lower, "webxr"):
		return "webxr", clean
	case strings.Contains(lower, "payment") || strings.Contains(lower, "apple pay"):
		return "payment", clean
	case strings.Contains(lower, "automation") || strings.Contains(lower, "web driver") || strings.Contains(lower, "webdriver"):
		return "automation", clean
	case strings.Contains(lower, "wireless playback"):
		return "wireless-playback", clean
	case strings.Contains(lower, "fullscreen") || strings.Contains(lower, "picture in picture"):
		return "fullscreen-pip", clean
	case strings.Contains(lower, "plugin") || strings.Contains(lower, "pdfkit") || strings.Contains(lower, "quicktime") || strings.Contains(lower, "youtube") || strings.Contains(lower, "java"):
		return "plugins", clean
	case strings.Contains(lower, "quirks"):
		return "quirks", clean
	case strings.Contains(lower, "libsecret"):
		return "libsecret", clean
	case strings.Contains(lower, "libnotify"):
		return "libnotify", clean
	case strings.Contains(lower, "systemd"):
		return "systemd", clean
	case strings.Contains(lower, "incrementalpdf") || strings.Contains(lower, "incremental pdf"):
		return "incremental-pdf", clean
	case strings.Contains(lower, "translation ui"):
		return "translation-ui", clean
	case strings.Contains(lower, "sleep disabler") || strings.Contains(lower, "sleepdisabler"):
		return "sleep-disablers", clean
	case strings.Contains(lower, "custom protocol"):
		return "custom-protocol", clean
	case strings.Contains(lower, "quick look"):
		return "quick-look", clean
	case strings.Contains(lower, "arkit") || strings.Contains(lower, "model element"):
		return "arkit-model", clean
	case strings.Contains(lower, "ftp"):
		return "ftp", clean
	case strings.Contains(lower, "watchos"):
		return "platform-watchos", clean
	case strings.Contains(lower, "touch bar"):
		return "touch-bar", clean
	case strings.Contains(lower, "playstation"):
		return "platform-playstation", clean
	case strings.Contains(lower, "gnu/hurd") || strings.Contains(lower, "hurd"):
		return "platform-hurd", clean
	case strings.Contains(lower, "unix"):
		return "platform-unix", clean
	case strings.Contains(lower, "web api statistics"):
		return "web-api-statistics", clean
	}

	if strings.HasPrefix(clean, "Initial commit") || strings.Contains(lower, "readme") {
		return "docs", clean
	}
	if strings.HasPrefix(strings.TrimSpace(subject), ":bug:") || strings.HasPrefix(lower, "fix") {
		return "maintenance", clean
	}
	slug := strings.Trim(nonSlug.ReplaceAllString(lower, "-"), "-")
	if slug == "" {
		slug = "misc"
	}
	return slug, clean
}
