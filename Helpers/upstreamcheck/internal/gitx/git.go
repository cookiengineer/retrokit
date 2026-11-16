package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a thin wrapper around the git command line. The upstream WebKit
// tree is far too large to make a pure-Go git implementation worthwhile for
// this tool, and the CLI already has the rename detection we need.
type Repo struct {
	Dir string
}

type Commit struct {
	Hash    string
	Subject string
	Parents []string
}

type DiffEntry struct {
	Status  byte
	OldPath string
	NewPath string
}

func Open(dir string) (*Repo, error) {
	r := &Repo{Dir: dir}
	if _, err := r.Run("rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", dir, err)
	}
	return r, nil
}

func (r *Repo) Run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (r *Repo) RunInput(input string, args ...string) (string, error) {
	return r.RunInputEnv(nil, input, args...)
}

func (r *Repo) RunInputEnv(env []string, input string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (r *Repo) RunEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (r *Repo) RevParse(ref string) (string, error) {
	out, err := r.Run("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) RefExists(ref string) bool {
	_, err := r.RevParse(ref)
	return err == nil
}

func (r *Repo) IsAncestor(a, b string) bool {
	_, err := r.Run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

func (r *Repo) RootCommit(ref string) (string, error) {
	out, err := r.Run("rev-list", "--max-parents=0", ref)
	if err != nil {
		return "", err
	}
	lines := strings.Fields(out)
	if len(lines) == 0 {
		return "", fmt.Errorf("no root commit for %s", ref)
	}
	return lines[len(lines)-1], nil
}

func (r *Repo) CurrentRef() string {
	if out, err := r.Run("symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	if out, err := r.RevParse("HEAD"); err == nil {
		return out
	}
	return ""
}

func (r *Repo) CommitInfo(hash string) (Commit, error) {
	out, err := r.Run("show", "-s", "--format=%H%x00%s%x00%P", hash)
	if err != nil {
		return Commit{}, err
	}
	parts := strings.Split(strings.TrimRight(out, "\n"), "\x00")
	if len(parts) < 3 {
		return Commit{}, fmt.Errorf("unexpected commit format for %s", hash)
	}
	c := Commit{Hash: parts[0], Subject: parts[1]}
	if strings.TrimSpace(parts[2]) != "" {
		c.Parents = strings.Fields(parts[2])
	}
	return c, nil
}

func (r *Repo) ListCommits(from, to string) ([]Commit, error) {
	out, err := r.Run("rev-list", "--reverse", from+".."+to)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		c, err := r.CommitInfo(line)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func (r *Repo) DiffNameStatus(from, to string, paths []string) ([]DiffEntry, error) {
	args := []string{"diff", "--name-status", "-M", "--find-renames=40%", from, to}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func (r *Repo) DiffTree(commit string, paths []string) ([]DiffEntry, error) {
	args := []string{"diff-tree", "-r", "-M", "--find-renames=40%", "--no-commit-id", "--name-status", commit}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func parseNameStatus(out string) []DiffEntry {
	var entries []DiffEntry
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0][0]
		if status == 'R' || status == 'C' {
			if len(fields) >= 3 {
				entries = append(entries, DiffEntry{Status: status, OldPath: fields[1], NewPath: fields[2]})
			}
			continue
		}
		entries = append(entries, DiffEntry{Status: status, OldPath: fields[1], NewPath: fields[1]})
	}
	return entries
}

func (r *Repo) ShowFile(ref, path string) ([]byte, error) {
	out, err := r.Run("show", ref+":"+path)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func (r *Repo) TreePaths(ref string, paths []string) (map[string]bool, error) {
	entries, err := r.TreeEntries(ref, paths)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(entries))
	for path := range entries {
		result[path] = true
	}
	return result, nil
}

type TreeEntry struct {
	Mode string
	Type string
	OID  string
	Path string
}

func (r *Repo) TreeEntries(ref string, paths []string) (map[string]TreeEntry, error) {
	args := []string{"ls-tree", "-r", ref}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]TreeEntry)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		result[path] = TreeEntry{Mode: fields[0], Type: fields[1], OID: fields[2], Path: path}
	}
	return result, nil
}

type IndexEntry struct {
	Mode string
	OID  string
	Path string
}

func (r *Repo) IndexEntries(env []string) (map[string]IndexEntry, error) {
	out, err := r.RunEnv(env, "ls-files", "-s")
	if err != nil {
		return nil, err
	}
	result := make(map[string]IndexEntry)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		result[path] = IndexEntry{Mode: fields[0], OID: fields[1], Path: path}
	}
	return result, nil
}

func (r *Repo) FileExistsAt(ref, path string) bool {
	_, err := r.Run("cat-file", "-e", ref+":"+path)
	return err == nil
}

func (r *Repo) BlobOID(ref, path string) (string, error) {
	out, err := r.Run("rev-parse", ref+":"+path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CommitTree creates a commit object with the given tree and parent.
func (r *Repo) CommitTree(tree, parent, message string) (string, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	out, err := r.RunInput(message, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) UpdateRef(ref, commit string) error {
	_, err := r.Run("update-ref", ref, commit)
	return err
}

func (r *Repo) Tag(name, commit, message string) error {
	args := []string{"tag", "-a", name, "-m", message, commit}
	_, err := r.Run(args...)
	return err
}

// Relative makes path relative to the repository root.
func (r *Repo) Relative(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(r.Dir, abs)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
