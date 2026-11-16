package index

import (
	"strings"
)

// BuildLines returns significant, non-comment lines from build files. These
// are compared between the baseline and the fork to remember removed source
// list entries, feature flags and preference keys.
func BuildLines(content []byte) []Line {
	raw := strings.Split(string(content), "\n")
	lines := make([]Line, 0, len(raw))
	for i, line := range raw {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//") {
			continue
		}
		if text == "{" || text == "}" || text == "};" || text == ")" || text == "(" {
			continue
		}
		if len(text) > 400 {
			text = text[:400]
		}
		lines = append(lines, Line{Number: i + 1, Text: text})
	}
	return lines
}
