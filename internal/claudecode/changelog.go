package claudecode

import (
	"strings"
)

// extractChangelogSection finds the section of a Keep-a-Changelog-style
// markdown file that corresponds to `version`. It returns the body of the
// section (without the heading) or "" if no section is found.
//
// Matches headings like:
//
//	## 2.1.114
//	## [2.1.114] - 2025-04-18
//	## v2.1.114
//	# 2.1.114
func extractChangelogSection(md, version string) string {
	lines := strings.Split(md, "\n")
	start, end := -1, len(lines)
	for i, ln := range lines {
		if start < 0 && isHeadingForVersion(ln, version) {
			start = i + 1
			continue
		}
		if start >= 0 && isAnyVersionHeading(ln) {
			end = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func isHeadingForVersion(line, version string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "#") {
		return false
	}
	s = strings.TrimLeft(s, "#")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimPrefix(s, "v")

	// possible tail after version: "] - 2025-04-18" or "- 2025-04-18" or ""
	if !strings.HasPrefix(s, version) {
		return false
	}
	rest := s[len(version):]
	if rest == "" {
		return true
	}
	switch rest[0] {
	case ' ', ']', '-', '\t':
		return true
	}
	return false
}

func isAnyVersionHeading(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "#") {
		return false
	}
	s = strings.TrimLeft(s, "#")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimPrefix(s, "v")
	// looks-like-version: starts with a digit
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}
