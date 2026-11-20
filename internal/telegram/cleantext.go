package telegram

import (
	"strings"
	"unicode"
)

// CleanText makes text from an untrusted upstream safe to place on one line
// of a message: C0/C1 control characters (ESC, NUL, BEL, newlines, …) and
// visually blank characters (U+2800 BRAILLE PATTERN BLANK) become spaces;
// invisible characters are dropped: Unicode format characters (bidi
// overrides such as U+202E, zero-width characters, the BOM), variation
// selectors, private-use characters, noncharacters and the other default-
// ignorable fillers (Hangul fillers U+115F/U+1160/U+3164/U+FFA0, U+034F
// COMBINING GRAPHEME JOINER, U+17B4/U+17B5). Whitespace runs, including NBSP
// and other Unicode spaces, collapse to one space.
//
// Control characters would otherwise reach terminals through --dry-run
// output, and Telegram can reject a message that carries them, failing every
// run until the upstream changes; bidi overrides can make a post read
// differently than it is, and blank-looking characters can make an empty
// field or a fake gap look like real content. CleanText does not escape:
// pass the result through EscapeMarkdownV2Plain (or the code/URL variants)
// as usual. Dropping variation selectors turns emoji presentation sequences
// such as U+2764 U+FE0F (red heart) into their base character, which is harmless.
func CleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == brailleBlank:
			return ' '
		case invisible(r):
			return -1
		default:
			return r
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

const brailleBlank = '\u2800'

// invisible reports whether r renders as nothing: format characters,
// variation selectors, private use, noncharacters and the default-ignorable
// fillers that are none of those.
func invisible(r rune) bool {
	switch r {
	case '\u034f', '\u115f', '\u1160', '\u17b4', '\u17b5', '\u3164', '\uffa0':
		return true
	}
	return unicode.In(r, unicode.Cf, unicode.Co, unicode.Variation_Selector, unicode.Noncharacter_Code_Point)
}
