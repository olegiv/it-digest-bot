package telegram

import (
	"strings"
	"unicode"
)

// CleanText makes text from an untrusted upstream safe to place on one line
// of a message: C0/C1 control characters (ESC, NUL, BEL, newlines, …) become
// spaces, Unicode format characters (bidi overrides such as U+202E,
// zero-width characters, the BOM) are dropped, and whitespace runs,
// including NBSP and other Unicode spaces, collapse to one space.
//
// Control characters would otherwise reach terminals through --dry-run
// output, and Telegram can reject a message that carries them, failing every
// run until the upstream changes; bidi overrides can make a post read
// differently than it is. CleanText does not escape: pass the result through
// EscapeMarkdownV2Plain (or the code/URL variants) as usual.
func CleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return ' '
		case unicode.Is(unicode.Cf, r):
			return -1
		default:
			return r
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
