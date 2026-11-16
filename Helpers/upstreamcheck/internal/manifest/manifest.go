package manifest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Feature struct {
	ID          string
	Title       string
	Status      string
	SecurityRef string
	Tokens      []string
}

type Entry struct {
	Feature     string
	Kind        string
	Symbol      string
	Path        string
	Disposition string
	Scope       string
	Origin      string
	Notes       string
}

const divergenceHeader = "# feature\tkind\tsymbol\tpath\tdisposition\tscope\torigin\tnotes"
const featuresHeader = "# id\ttitle\tstatus\tsecurity_ref\ttokens"

func LoadFeatures(path string) ([]Feature, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var features []Feature
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			return nil, fmt.Errorf("invalid feature line: %q", line)
		}
		feature := Feature{ID: fields[0], Title: fields[1], Status: fields[2]}
		if len(fields) > 3 {
			feature.SecurityRef = fields[3]
		}
		if len(fields) > 4 && fields[4] != "" {
			for _, token := range strings.Split(fields[4], ",") {
				token = strings.TrimSpace(token)
				if token != "" {
					feature.Tokens = append(feature.Tokens, token)
				}
			}
		}
		features = append(features, feature)
	}
	return features, scanner.Err()
}

func WriteFeatures(path string, features []Feature) error {
	features = append([]Feature(nil), features...)
	sort.SliceStable(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	var b strings.Builder
	b.WriteString(featuresHeader)
	b.WriteByte('\n')
	for _, feature := range features {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			clean(feature.ID), clean(feature.Title), clean(feature.Status),
			clean(feature.SecurityRef), clean(strings.Join(feature.Tokens, ",")))
	}
	return writeFile(path, b.String())
}

func LoadEntries(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		for len(fields) < 8 {
			fields = append(fields, "")
		}
		entries = append(entries, Entry{
			Feature:     fields[0],
			Kind:        fields[1],
			Symbol:      fields[2],
			Path:        fields[3],
			Disposition: fields[4],
			Scope:       fields[5],
			Origin:      fields[6],
			Notes:       fields[7],
		})
	}
	return entries, scanner.Err()
}

func WriteEntries(path string, entries []Entry) error {
	entries = Dedup(entries)
	var b strings.Builder
	b.WriteString(divergenceHeader)
	b.WriteByte('\n')
	for _, entry := range entries {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			clean(entry.Feature), clean(entry.Kind), clean(entry.Symbol),
			clean(entry.Path), clean(entry.Disposition), clean(entry.Scope),
			clean(entry.Origin), clean(entry.Notes))
	}
	return writeFile(path, b.String())
}

func Dedup(entries []Entry) []Entry {
	type key struct{ feature, kind, symbol, path, disposition string }
	seen := make(map[key]int)
	var result []Entry
	for _, entry := range entries {
		k := key{entry.Feature, entry.Kind, entry.Symbol, entry.Path, entry.Disposition}
		if idx, ok := seen[k]; ok {
			if result[idx].Origin == "" {
				result[idx].Origin = entry.Origin
			}
			if entry.Notes != "" && !strings.Contains(result[idx].Notes, entry.Notes) {
				if result[idx].Notes != "" {
					result[idx].Notes += "; "
				}
				result[idx].Notes += entry.Notes
			}
			continue
		}
		seen[k] = len(result)
		result = append(result, entry)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Feature != result[j].Feature {
			return result[i].Feature < result[j].Feature
		}
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Symbol != result[j].Symbol {
			return result[i].Symbol < result[j].Symbol
		}
		return result[i].Disposition < result[j].Disposition
	})
	return result
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// CountByFeature returns counts grouped by feature and disposition.
func CountByFeature(entries []Entry) map[string]map[string]int {
	result := make(map[string]map[string]int)
	for _, entry := range entries {
		if result[entry.Feature] == nil {
			result[entry.Feature] = make(map[string]int)
		}
		result[entry.Feature][entry.Disposition]++
	}
	return result
}
