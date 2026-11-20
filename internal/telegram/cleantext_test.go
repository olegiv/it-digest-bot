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
