package claudecode

import (
	"strings"
	"testing"
)

func TestExtractChangelogSection(t *testing.T) {
	t.Parallel()
	md := `# Changelog

All notable changes to this project.

## 2.1.114 - 2025-04-18
- foo feature
- bug fix

## 2.1.113 - 2025-04-17
- older

## [2.1.112]
- much older
`

	cases := []struct {
		version string
		want    []string
		absent  []string
	}{
		{"2.1.114", []string{"foo feature", "bug fix"}, []string{"- older", "- much older"}},
		{"2.1.113", []string{"- older"}, []string{"foo feature", "much older"}},
		{"2.1.112", []string{"much older"}, []string{"- older", "foo feature"}},
		{"9.9.9", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			body := extractChangelogSection(md, tc.version)
			if len(tc.want) == 0 {
				if body != "" {
					t.Errorf("expected empty body for unknown version, got %q", body)
				}
				return
			}
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("missing %q in:\n%s", w, body)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(body, a) {
					t.Errorf("unexpected %q in:\n%s", a, body)
				}
			}
		})
	}
}

func TestExtractChangelogSectionWithVPrefix(t *testing.T) {
	t.Parallel()
	md := "## v1.0.0\n- launch\n"
	body := extractChangelogSection(md, "1.0.0")
	if !strings.Contains(body, "launch") {
		t.Errorf("v-prefix match failed: %q", body)
	}
}
