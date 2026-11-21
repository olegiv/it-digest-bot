package telegram

import (
	"testing"
	"unicode"
)

func TestCleanText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"  plain   text ", "plain text"},
		{"a\tb\r\nc\x00d\x7fe\u0085f", "a b c d e f"},
		{"Heap\x1b[31m overflow\x07", "Heap [31m overflow"},
		{"x\u202e()\u200b", "x()"},
		{"\u200d\u2060\u00ad\ufeff", ""},
		{"text\u202dhere", "texthere"},
		{"non\u00a0breaking\u2009thin\u2028line", "non breaking thin line"},
		{"e\u0301", "e\u0301"}, // combining marks are kept
		{"\u65e5\u672c\u8a9e \U0001F418", "\u65e5\u672c\u8a9e \U0001F418"},
		{"", ""},
	} {
		if got := CleanText(tc.in); got != tc.want {
			t.Errorf("CleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanTextLeavesNoControlOrFormatRunes(t *testing.T) {
	t.Parallel()
	var all []rune
	for r := rune(0); r < 0x3000; r++ {
		all = append(all, r)
	}
	all = append(all, 0xfeff, 0xfff9, 0xe0001)
	for _, r := range CleanText(string(all)) {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("CleanText left %U", r)
		}
	}
}

// TestCleanTextInvisibleAndBlank covers CLN-001: characters that render as
// nothing (or as a blank) must not survive into a post.
func TestCleanTextInvisibleAndBlank(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"a\u3164b", "ab"},                               // Hangul filler
		{"a\u115f\u1160b\uffa0c", "abc"},                 // other Hangul fillers
		{"a\u2800b", "a b"},                              // Braille blank reads as a space
		{"\u2800\u2800", ""},                             // a field of blanks is empty
		{"a\ufe0fb\ufe00c", "abc"},                       // variation selectors
		{"a\U000E0100b", "ab"},                           // ideographic variation selector
		{"a\ue000b\U000F0000c", "abc"},                   // private use
		{"a\ufffeb\U0010FFFFc", "abc"},                   // noncharacters
		{"a\u034fb\u17b4c\u17b5d", "abcd"},               // CGJ, Khmer inherent vowels
		{"heart \u2764\ufe0f", "heart \u2764"},           // emoji keeps its base character
		{"e\u0301 \u65e5\u672c", "e\u0301 \u65e5\u672c"}, // real marks and text stay
	} {
		if got := CleanText(tc.in); got != tc.want {
			t.Errorf("CleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	var all []rune
	for r := rune(0); r <= 0x10FFFF; r += 7 {
		if r < 0xD800 || r > 0xDFFF {
			all = append(all, r)
		}
	}
	all = append(all, 0x3164, 0x2800, 0xFE0F, 0xE0100, 0xE000, 0xFFFF, 0x115F, 0x034F)
	for _, r := range CleanText(string(all)) {
		if unicode.IsControl(r) || r == 0x2800 || invisible(r) {
			t.Fatalf("CleanText left %U", r)
		}
	}
}
