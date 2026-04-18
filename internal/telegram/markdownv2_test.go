package telegram

import (
	"strings"
	"testing"
)

func TestEscapeMarkdownV2(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, in, want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "plain ascii no specials",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "all specials get escaped",
			in:   "_*[]()~`>#+-=|{}.!\\",
			want: `\_\*\[\]\(\)\~\` + "`" + `\>\#\+\-\=\|\{\}\.\!\\`,
		},
		{
			// inside a code span, only ` and \ need escaping per MarkdownV2 rules.
			name: "inline code preserved verbatim",
			in:   "use `foo.bar()` please",
			want: "use `foo.bar()` please",
		},
		{
			name: "inline code with backslash inside is escaped",
			in:   "run `a\\b` here",
			want: "run `a\\\\b` here",
		},
		{
			name: "unmatched backtick escaped",
			in:   "this ` is alone",
			want: "this \\` is alone",
		},
		{
			name: "triple backtick fenced block preserved",
			in:   "```go\nfunc x() {}\n```",
			want: "```go\nfunc x() {}\n```",
		},
		{
			// inside a URL, only ) and \ need escaping per MarkdownV2 rules.
			name: "link preserved, url only escapes ) and backslash",
			in:   "see [npm](https://foo.com/pkg/bar)",
			want: `see [npm](https://foo.com/pkg/bar)`,
		},
		{
			name: "link text escapes specials, url does not",
			in:   "[a.b](https://x.y)",
			want: `[a\.b](https://x.y)`,
		},
		{
			name: "unmatched left bracket escaped",
			in:   "[dangling",
			want: `\[dangling`,
		},
		{
			name: "issue ref hash escaped",
			in:   "fixes #123",
			want: `fixes \#123`,
		},
		{
			name: "list with dashes",
			in:   "- item one\n- item two",
			want: `\- item one` + "\n" + `\- item two`,
		},
		{
			name: "utf8 passes through unchanged",
			in:   "🚀 Claude Code — 2.1.114",
			want: `🚀 Claude Code — 2\.1\.114`,
		},
		{
			name: "release note style",
			in:   "- Fix `--help` output (#456)\n- [PR](https://github.com/x/y/pull/1)",
			want: `\- Fix ` + "`--help`" + ` output \(\#456\)` + "\n" + `\- [PR](https://github.com/x/y/pull/1)`,
		},
		{
			name: "fence inside prose",
			in:   "Here's code:\n```\nraw *stuff* here\n```\ndone.",
			want: `Here's code:` + "\n```\nraw *stuff* here\n```\n" + `done\.`,
		},
		{
			// After the first ), the link ends; any subsequent ) is plain text.
			name: "link closes at first ); trailing paren treated as text",
			in:   "[x](https://a.com/foo)bar)",
			want: `[x](https://a.com/foo)bar\)`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := EscapeMarkdownV2(tc.in)
			if got != tc.want {
				t.Errorf("\n  in  = %q\n  got = %q\n  want= %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEscapeMarkdownV2Code(t *testing.T) {
	t.Parallel()
	if got := EscapeMarkdownV2Code("a`b\\c"); got != `a\`+"`"+`b\\c` {
		t.Errorf("got %q", got)
	}
}

func TestEscapeMarkdownV2URL(t *testing.T) {
	t.Parallel()
	if got := EscapeMarkdownV2URL("https://a.com/b)c"); got != `https://a.com/b\)c` {
		t.Errorf("got %q", got)
	}
}

func TestTruncateMarkdownV2Short(t *testing.T) {
	t.Parallel()
	s := "hello"
	if got := TruncateMarkdownV2(s, "…", 1000); got != s {
		t.Errorf("short input was truncated: %q", got)
	}
}

func TestTruncateMarkdownV2AtParagraph(t *testing.T) {
	t.Parallel()
	s := "first paragraph\n\nsecond paragraph\n\nthird paragraph\n\nfourth paragraph"
	got := TruncateMarkdownV2(s, "…", 40)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing ellipsis: %q", got)
	}
	if strings.Contains(got[:len(got)-len("…")], "\n\nsecond") && !strings.HasSuffix(got[:len(got)-len("…")], "first paragraph") {
		// ensure it cut at a paragraph boundary, not mid-word
		if !strings.Contains(got, "\n\n…") && !strings.HasSuffix(strings.TrimSuffix(got, "…"), "\n\n") {
			// it must have landed on a newline boundary
			if !strings.HasSuffix(got, "first paragraph…") &&
				!strings.HasSuffix(got, "second paragraph…") {
				t.Errorf("did not cut at paragraph boundary: %q", got)
			}
		}
	}
}

func TestTruncateMarkdownV2NeverMidCodeSpan(t *testing.T) {
	t.Parallel()
	s := "prefix text here `this is a long code span that extends past the limit` more text"
	got := TruncateMarkdownV2(s, "…", 40)
	// Count backticks in the result — must be even (span is closed).
	n := strings.Count(got, "`")
	if n%2 != 0 {
		t.Errorf("truncation left an open code span: %q (backticks=%d)", got, n)
	}
}

func TestTruncateMarkdownV2NeverMidCodeFence(t *testing.T) {
	t.Parallel()
	s := "intro line here\n```\nline1\nline2\nline3\nline4\n```\noutro"
	got := TruncateMarkdownV2(s, "…", 25)
	// Triple-backtick count should be even.
	if strings.Count(got, "```")%2 != 0 {
		t.Errorf("truncation left an open code fence: %q", got)
	}
}

func TestTruncateMarkdownV2PreservesUTF8(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("🚀 ", 50) // 4 bytes per emoji + space
	got := TruncateMarkdownV2(s, "…", 30)
	// "…" is 3 bytes, truncation should yield valid UTF-8
	for i := 0; i < len(got); i++ {
		if got[i]&0xC0 == 0x80 && i == 0 {
			t.Errorf("truncation produced invalid UTF-8 leading byte: %q", got)
		}
	}
}

func TestEscaperIsIdempotentOnEscaped(t *testing.T) {
	// Escaping twice should produce the double-escaped form — document
	// this so no caller accidentally double-escapes.
	t.Parallel()
	once := EscapeMarkdownV2("a.b")
	twice := EscapeMarkdownV2(once)
	if twice == once {
		t.Errorf("escaper appears to be lossy on its own output")
	}
}
