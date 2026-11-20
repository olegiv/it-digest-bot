package telegram

import "strings"

// WellFormedMarkdownV2 reports whether s is well formed in the MarkdownV2
// subset the Escape* functions in this package emit: escaped characters,
// non-empty inline code spans and fenced blocks, and [text](url) links that
// are not nested. Any other unescaped special character, an unterminated
// entity or a dangling backslash makes it false.
//
// Telegram rejects a whole message when one entity is malformed, so callers
// that build text from untrusted input (the daily digest) check each piece
// with this and fall back to EscapeMarkdownV2Plain, which is always well
// formed, when it fails.
func WellFormedMarkdownV2(s string) bool { return wellFormed(s, false) }

func wellFormed(s string, inLink bool) bool {
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\':
			if i+1 >= len(s) {
				return false
			}
			i += 2
		case strings.HasPrefix(s[i:], "```"):
			end := closeCode(s, i+3, "```")
			if end <= i+3 {
				return false
			}
			i = end + 3
		case c == '`':
			end := closeCode(s, i+1, "`")
			if end <= i+1 || strings.Contains(s[i+1:end], "\n") {
				return false
			}
			i = end + 1
		case c == '[':
			if inLink {
				return false
			}
			next, ok := wellFormedLink(s, i)
			if !ok {
				return false
			}
			i = next
		case c < 128 && mdv2Specials[c]:
			return false
		default:
			i++
		}
	}
	return true
}

// closeCode returns the index of the closing delimiter of a code entity
// whose body starts at s[from], or -1. Inside code only ` and \ are
// escaped, so a backslash always consumes the next byte.
func closeCode(s string, from int, delim string) int {
	for j := from; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if strings.HasPrefix(s[j:], delim) {
			return j
		}
	}
	return -1
}

// wellFormedLink checks the [text](url) starting at s[i] and returns the
// index just past it.
func wellFormedLink(s string, i int) (int, bool) {
	j := i + 1
	for ; j < len(s) && s[j] != ']'; j++ {
		switch s[j] {
		case '\\':
			j++
		case '`':
			end := closeCode(s, j+1, "`")
			if end < 0 {
				return 0, false
			}
			j = end
		}
	}
	if j >= len(s) || j == i+1 || !wellFormed(s[i+1:j], true) {
		return 0, false
	}
	if j+1 >= len(s) || s[j+1] != '(' {
		return 0, false
	}
	k := j + 2
	for ; k < len(s) && s[k] != ')'; k++ {
		switch s[k] {
		case '\\':
			k++
		case '\n':
			return 0, false
		}
	}
	if k >= len(s) || k == j+2 {
		return 0, false
	}
	return k + 1, true
}
