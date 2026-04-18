package claudecode

import (
	"strings"
	"testing"
)

func TestFormatReleaseContainsKeyParts(t *testing.T) {
	t.Parallel()
	out := FormatRelease("2.1.114",
		"- Added `--foo` flag\n- Fixed bug",
		"https://github.com/anthropics/claude-code/releases/tag/v2.1.114",
		"https://www.npmjs.com/package/@anthropic-ai/claude-code")

	for _, want := range []string{
		"🚀",
		"Claude Code",
		"`2.1.114`",
		"npm",
		"GitHub",
		"\\#ClaudeCode",
		"\\#AI",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestFormatReleaseTruncatesLongNotes(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("line of notes\n", 400) // ~5600 bytes
	out := FormatRelease("1.0.0", long, "https://example.com/release", "https://npm/pkg")
	if !strings.Contains(out, "📖 Full notes:") {
		t.Errorf("expected truncation footer in output:\n%s", out)
	}
	if len(out) > 4096 {
		t.Errorf("output exceeds 4096 bytes: %d", len(out))
	}
}

func TestFormatReleaseShortNotes(t *testing.T) {
	t.Parallel()
	out := FormatRelease("1.0.0", "tiny", "https://example.com/rel", "https://npm/pkg")
	if strings.Contains(out, "Full notes:") {
		t.Errorf("did not expect truncation footer for short notes:\n%s", out)
	}
}

func TestNPMPackageURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"@anthropic-ai/claude-code", "https://www.npmjs.com/package/@anthropic-ai%2Fclaude-code"},
		{"plain-package", "https://www.npmjs.com/package/plain-package"},
		{"weird name with spaces", "https://www.npmjs.com/package/weird%20name%20with%20spaces"},
		{"query?injection#frag", "https://www.npmjs.com/package/query%3Finjection%23frag"},
	}
	for _, tc := range cases {
		if got := NPMPackageURL(tc.in); got != tc.want {
			t.Errorf("NPMPackageURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
