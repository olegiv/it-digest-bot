package telegram

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// escapers lists every escaper whose output is placed into a message as a
// complete fragment. Each must yield well-formed MarkdownV2 for any input.
var escapers = map[string]func(string) string{
	"EscapeMarkdownV2":      EscapeMarkdownV2,
	"EscapeMarkdownV2Plain": EscapeMarkdownV2Plain,
	"LinkText":              EscapeMarkdownV2LinkText,
	"Links(deny)":           func(s string) string { return EscapeMarkdownV2Links(s, nil) },
	"Links(ok.example)": func(s string) string {
		return EscapeMarkdownV2Links(s, func(u string) bool { return strings.Contains(u, "ok.example") })
	},
}

func TestEscapersWellFormedRegressions(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		// MD-001: a removed link between an empty span and a span used to
		// leave "```x`", an unterminated pre entity.
		"``https://evil.com`x`",
		"``[https://evil.com](https://evil.com)`x`",
		"``[a](https://evil.com)``x`",
		"``````",
		"`` `` ``",
		// MD-005: nested links.
		"[![CI](https://img.example/x)](https://ok.example/y)",
		"[a [b](https://ok.example/1) c](https://ok.example/2)",
		"trailing \\",
		"[0](\\\n)", // fuzz: a backslash must not let a link span lines
		"[x](https://ok.example/a\\)",
	} {
		for name, esc := range escapers {
			if out := esc(in); !WellFormedMarkdownV2(out) {
				t.Errorf("%s(%q) = %q is not well formed", name, in, out)
			}
		}
	}
}

func TestEscapersWellFormedRandom(t *testing.T) {
	t.Parallel()
	pieces := []string{
		"`", "``", "```", "[", "]", "(", ")", "\\", "*", "_", "~", "|", ">", "#", "+", "-", "=", "{", "}", ".", "!",
		" ", "\n", "a", "Z", "1", "é", "https://", "http://", "tg://", "www.", "ok.example", "evil.com", "/", ":",
		"[t](https://ok.example/p)", "[t](https://evil.com/p)", "`c`",
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		var sb strings.Builder
		for range r.IntN(24) {
			sb.WriteString(pieces[r.IntN(len(pieces))])
		}
		in := sb.String()
		for name, esc := range escapers {
			if out := esc(in); !WellFormedMarkdownV2(out) {
				t.Fatalf("%s(%q) = %q is not well formed", name, in, out)
			}
		}
	}
}

func FuzzEscapersWellFormed(f *testing.F) {
	for _, seed := range []string{"``https://evil.com`x`", "[a](https://b)", "```x```", "plain"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for name, esc := range escapers {
			if out := esc(in); !WellFormedMarkdownV2(out) {
				t.Fatalf("%s(%q) = %q is not well formed", name, in, out)
			}
		}
	})
}

func TestWellFormedMarkdownV2(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"plain \\. text", true},
		{"`code`", true},
		{"```pre\nblock```", true},
		{"[a](https://x.example/)", true},
		{"[a `c`](https://x.example/)", true},
		{"``", false},
		{"``````", false},
		{"```x`", false},
		{"`a\nb`", false},
		{"a.b", false},
		{"dangling \\", false},
		{"[a](b", false},
		{"[](https://x)", false},
		{"[a]()", false},
		{"[a](b\nc)", false},
		{"[[a](b)](c)", false},
		{"[a] b", false},
		{"*bold*", false},
	} {
		if got := WellFormedMarkdownV2(tc.in); got != tc.want {
			t.Errorf("WellFormedMarkdownV2(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBareLinkTokensDropped(t *testing.T) {
	t.Parallel()
	allow := func(u string) bool {
		return strings.HasPrefix(strings.ToLower(u), "https://ok.example/") || strings.HasPrefix(strings.ToLower(u), "http://www.ok.example/")
	}
	for _, tc := range []struct{ in, want string }{
		// MD-002: tokens after _, letters, digits and '/' and non-http schemes.
		{"_https://evil.com/x_", "\\_"}, // the token runs to whitespace
		{"a/https://evil.com", "a/"},
		{"1https://evil.com x", "1 x"},
		{"see tg://resolve?domain=evil now", "see  now"},
		{"pay ton://transfer/EQ now", "pay  now"},
		{"ftp://evil.com/f and sftp://x", " and "},
		{"WWW.evil.com/x.", "\\."},
		{"go www.ok.example/a", "go www\\.ok\\.example/a"},
		{"go https://ok.example/a", "go https://ok\\.example/a"},
		{"word www", "word www"},
		{"awww.evil.com", "awww\\.evil\\.com"},
		{"tg:// alone", "tg:// alone"},
		{"`tg://kept-in-code`", "`tg://kept-in-code`"},
	} {
		if got := EscapeMarkdownV2Links(tc.in, allow); got != tc.want {
			t.Errorf("EscapeMarkdownV2Links(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeMarkdownV2EmptyCodeAndNestedLinks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"a `` b", "a \\`\\` b"},
		{"``````", "\\`\\`\\`\\`\\`\\`"},
		{"[![CI](https://img/x)](https://ok/y)", "[\\!CI](https://ok/y)"},
		{"`c` [l](https://x/) ```p```", "`c` [l](https://x/) ```p```"},
	} {
		if got := EscapeMarkdownV2(tc.in); got != tc.want {
			t.Errorf("EscapeMarkdownV2(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBareLinkTokenEndsAtNonURLCharacters(t *testing.T) {
	t.Parallel()
	allow := func(u string) bool { return strings.HasPrefix(u, "https://ok.example/") }
	for _, tc := range []struct{ in, want string }{
		{"https://ok.example/p[HTTP://EVIL.COM/x", "https://ok\\.example/p\\["},
		{"https://ok.example/p\\tg://evil", "https://ok\\.example/p\\\\"},
		{"https://ok.example/p|ton://evil/x", "https://ok\\.example/p\\|"},
		{"https://ok.example/p{x}", "https://ok\\.example/p\\{x\\}"},
	} {
		if got := EscapeMarkdownV2Links(tc.in, allow); got != tc.want {
			t.Errorf("EscapeMarkdownV2Links(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestLinkRemovalCannotGlueNewLinks: removing a denied link keeps its text
// and removing a bare token closes the gap, so neighbours can join into a
// new link token. EscapeMarkdownV2Links filters until nothing more is
// removed, and a dangling prefix before an entity is dropped.
func TestLinkRemovalCannotGlueNewLinks(t *testing.T) {
	t.Parallel()
	allow := func(u string) bool { return strings.HasPrefix(u, "https://ok.example/") }
	for _, tc := range []struct{ in, want string }{
		// The link text glues onto "tg:" and the whole token is dropped.
		{"tg:[//evil.example](https://bad.example/)", ""},
		// "ttps://…" is itself a scheme token, dropped inside the link text.
		{"h[ttps://evil.example](https://bad.example/) x", "h x"},
		{"tg:/https://bad.example/ /evil", "tg:/ /evil"},
		{"tg://`evil.example`", "`evil.example`"},
		{"www.[evil.example](https://ok.example/a)", "[evil\\.example](https://ok.example/a)"},
		{"HTTPS://[x](https://ok.example/a)", "[x](https://ok.example/a)"},
		{"[a](https://bad.example/)[b](https://bad.example/)", "ab"},
	} {
		got := EscapeMarkdownV2Links(tc.in, allow)
		if got != tc.want {
			t.Errorf("EscapeMarkdownV2Links(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(strings.ReplaceAll(got, "\\", ""), "evil.example") && !strings.Contains(got, "`") && !strings.Contains(got, "](https://ok.example/") {
			t.Errorf("EscapeMarkdownV2Links(%q) = %q leaves an off-host URL in plain text", tc.in, got)
		}
	}
}
