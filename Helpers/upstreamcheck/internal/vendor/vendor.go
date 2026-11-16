package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
)

type Options struct {
	UpstreamDir string
	Version     string
	Paths       []string
	BaseTag     string
	BaseFrom    string
	TargetTag   string
}

type Result struct {
	BaseCommit   string
	TargetCommit string
	BaseTag      string
	TargetTag    string
	UpstreamHead string
	Corrected    int
}

var versionRE = regexp.MustCompile(`SET_PROJECT_VERSION\(\s*([0-9]+)\s+([0-9]+)\s+([0-9]+)\s*\)`)

// Import creates a filtered upstream snapshot commit and tag. The fork's
// working tree is not touched; only refs under refs/tags are created.
func Import(repo *gitx.Repo, opts Options) (*Result, error) {
	if opts.UpstreamDir == "" {
		return nil, fmt.Errorf("upstream directory is required")
	}
	upstream, err := gitx.Open(opts.UpstreamDir)
	if err != nil {
		return nil, err
	}
	head, err := upstream.RevParse("HEAD")
	if err != nil {
		return nil, err
	}

	versionContent, err := upstream.ShowFile("HEAD", "Source/cmake/OptionsGTK.cmake")
	if err != nil {
		return nil, fmt.Errorf("read upstream version: %w", err)
	}
	match := versionRE.FindSubmatch(versionContent)
	if match == nil {
		return nil, fmt.Errorf("could not find SET_PROJECT_VERSION in upstream OptionsGTK.cmake")
	}
	actual := strings.Join([]string{string(match[1]), string(match[2]), string(match[3])}, ".")
	if opts.Version != "" && actual != opts.Version {
		return nil, fmt.Errorf("upstream is %s but --version is %s", actual, opts.Version)
	}
	if opts.Version == "" {
		opts.Version = actual
	}
	if opts.TargetTag == "" {
		opts.TargetTag = "vendor/" + opts.Version
	}
	if opts.BaseTag == "" {
		return nil, fmt.Errorf("base tag is required")
	}

	// Refuse to snapshot a dirty tree: the vendor commit must be reproducible
	// from the upstream commit, not from local modifications.
	dirtyArgs := []string{"status", "--porcelain", "--"}
	dirtyArgs = append(dirtyArgs, opts.Paths...)
	if dirty, err := upstream.Run(dirtyArgs...); err == nil && strings.TrimSpace(dirty) != "" {
		return nil, fmt.Errorf("upstream worktree is dirty for vendor paths:\n%s", dirty)
	}

	result := &Result{BaseTag: opts.BaseTag, TargetTag: opts.TargetTag, UpstreamHead: head}

	baseCommit, err := repo.RevParse(opts.BaseTag)
	if err != nil {
		if opts.BaseFrom == "" {
			return nil, fmt.Errorf("base tag %s does not exist and --base-from was not provided", opts.BaseTag)
		}
		baseCommit, err = createFilteredBase(repo, opts.BaseFrom, opts.BaseTag, opts.Paths, opts.Version)
		if err != nil {
			return nil, err
		}
	}
	result.BaseCommit = baseCommit

	targetCommit, corrected, err := createFilteredTarget(repo, upstream, opts.TargetTag, opts.Version, baseCommit, opts.Paths)
	if err != nil {
		return nil, err
	}
	result.TargetCommit = targetCommit
	result.Corrected = corrected
	return result, nil
}

func createFilteredBase(repo *gitx.Repo, from, tag string, paths []string, version string) (string, error) {
	indexFile, cleanup, err := tempIndex()
	if err != nil {
		return "", err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + indexFile}

	if _, err := repo.RunEnv(env, "read-tree", from); err != nil {
		return "", err
	}
	all, err := repo.TreePaths(from, nil)
	if err != nil {
		return "", err
	}
	var remove []string
	for path := range all {
		if !inPaths(path, paths) {
			remove = append(remove, path)
		}
	}
	if len(remove) > 0 {
		list, cleanupList, err := tempPathList(remove)
		if err != nil {
			return "", err
		}
		defer cleanupList()
		data, err := os.ReadFile(list)
		if err != nil {
			return "", err
		}
		if _, err := repo.RunInputEnv(env, string(data), "update-index", "--force-remove", "-z", "--stdin"); err != nil {
			return "", err
		}
	}
	tree, err := repo.RunEnv(env, "write-tree")
	if err != nil {
		return "", err
	}
	message := fmt.Sprintf("Vendor import: filtered WebKit baseline from %s\n\nFiltered paths: %s\n", from, strings.Join(paths, ", "))
	commit, err := repo.CommitTree(strings.TrimSpace(tree), "", message)
	if err != nil {
		return "", err
	}
	if err := repo.UpdateRef("refs/tags/"+tag, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func createFilteredTarget(repo *gitx.Repo, upstream *gitx.Repo, tag, version, parent string, paths []string) (string, int, error) {
	indexFile, cleanup, err := tempIndex()
	if err != nil {
		return "", 0, err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + indexFile}

	if _, err := repo.RunEnv(env, "read-tree", "--empty"); err != nil {
		return "", 0, err
	}
	tracked, err := upstream.TreeEntries("HEAD", paths)
	if err != nil {
		return "", 0, err
	}
	var files []string
	for path := range tracked {
		files = append(files, path)
	}
	if len(files) == 0 {
		return "", 0, fmt.Errorf("no files matched vendor paths")
	}
	list, cleanupList, err := tempPathList(files)
	if err != nil {
		return "", 0, err
	}
	defer cleanupList()

	addArgs := []string{"--work-tree=" + upstream.Dir, "add", "-f",
		"--pathspec-from-file=" + list, "--pathspec-file-nul"}
	if _, err := repo.RunEnv(env, addArgs...); err != nil {
		return "", 0, err
	}

	// git add may normalize line endings according to .gitattributes. The
	// vendor snapshot must be byte-identical to the upstream commit, so
	// compare index OIDs with upstream OIDs and replace any filtered blobs
	// with the exact upstream object.
	corrected, err := correctNormalizedBlobs(repo, upstream, env, tracked)
	if err != nil {
		return "", 0, err
	}

	tree, err := repo.RunEnv(env, "write-tree")
	if err != nil {
		return "", 0, err
	}
	message := fmt.Sprintf("Vendor import: WebKitGTK %s\n\nUpstream commit: %s\nFiltered paths: %s\n",
		version, mustHead(upstream), strings.Join(paths, ", "))
	commit, err := repo.CommitTree(strings.TrimSpace(tree), parent, message)
	if err != nil {
		return "", 0, err
	}
	if err := repo.UpdateRef("refs/tags/"+tag, commit); err != nil {
		return "", 0, err
	}
	return commit, corrected, nil
}

func correctNormalizedBlobs(repo *gitx.Repo, upstream *gitx.Repo, env []string, tracked map[string]gitx.TreeEntry) (int, error) {
	index, err := repo.IndexEntries(env)
	if err != nil {
		return 0, err
	}
	corrected := 0
	for path, want := range tracked {
		got, ok := index[path]
		if !ok || got.OID == want.OID {
			continue
		}
		content, err := upstream.ShowFile("HEAD", path)
		if err != nil {
			return corrected, err
		}
		oid, err := repo.RunInput(string(content), "hash-object", "-w", "--stdin")
		if err != nil {
			return corrected, err
		}
		oid = strings.TrimSpace(oid)
		if _, err := repo.RunEnv(env, "update-index", "--add", "--cacheinfo", want.Mode, oid, path); err != nil {
			return corrected, err
		}
		corrected++
	}
	return corrected, nil
}

func mustHead(repo *gitx.Repo) string {
	head, err := repo.RevParse("HEAD")
	if err != nil {
		return "unknown"
	}
	return head
}

func inPaths(path string, paths []string) bool {
	path = filepath.ToSlash(path)
	for _, prefix := range paths {
		prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func tempIndex() (string, func(), error) {
	f, err := os.CreateTemp("", "upstreamcheck-index-*")
	if err != nil {
		return "", nil, err
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return name, func() { os.Remove(name) }, nil
}

func tempPathList(paths []string) (string, func(), error) {
	f, err := os.CreateTemp("", "upstreamcheck-paths-*")
	if err != nil {
		return "", nil, err
	}
	name := f.Name()
	for _, path := range paths {
		if _, err := f.WriteString(path + "\x00"); err != nil {
			f.Close()
			os.Remove(name)
			return "", nil, err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", nil, err
	}
	return name, func() { os.Remove(name) }, nil
}
