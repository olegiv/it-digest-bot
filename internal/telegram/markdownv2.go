package telegram

import (
	"strings"
	"unicode/utf8"
)

// MarkdownV2 special characters per Telegram Bot API docs.
// Inside regular text, each of these must be prefixed with '\' unless it's
// intended as formatting.
var mdv2Specials = [128]bool{
	'_': true, '*': true, '[': true, ']': true, '(': true, ')': true,
	'~': true, '`': true, '>': true, '#': true, '+': true, '-': true,
	'=': true, '|': true, '{': true, '}': true, '.': true, '!': true,
	'\\': true,
}

// EscapeMarkdownV2 escapes `s` so Telegram's MarkdownV2 parser treats it as
// plain text — except for two constructs that are preserved verbatim:
//
//   - inline code spans:     `...`
//   - fenced code blocks:    ```...```
//   - link syntax:           [text](url)
//
// Inside a code span or fence, only ` and \ are escaped. Inside a link's
// URL, only ) and \ are escaped. Inside a link's text, the regular rules
// apply.
//
// An unmatched backtick or `[` is escaped as literal.
func EscapeMarkdownV2(s string) string { return escapeMarkdownV2(s, nil, false) }

// EscapeMarkdownV2Links is EscapeMarkdownV2 with a link policy, for prose
// derived from untrusted input (feed items and the model's summaries of
// them). Code spans, fences and the regular escaping work as in
// EscapeMarkdownV2, but:
//
//   - a [text](url) link stays live only when allow(url) reports true;
//     otherwise only its text is kept, escaped by the same rules;
//   - a bare http:// or https:// URL outside code spans and links, which
//     Telegram clients turn into a link on their own, is dropped unless
//     allow(url) reports true;
//   - links inside a kept link's text become plain text (Telegram rejects
//     nested links).
//
// A nil allow rejects every link. Bare domain names without a scheme
// ("example.com") are left as text; Telegram clients may still link them.
func EscapeMarkdownV2Links(s string, allow func(url string) bool) string {
	if allow == nil {
		allow = denyLinks
	}
	return escapeMarkdownV2(s, allow, true)
}

// EscapeMarkdownV2LinkText escapes s for use as the text of a [text](url)
// link: like EscapeMarkdownV2, but links inside it become plain text,
// because Telegram rejects nested links.
func EscapeMarkdownV2LinkText(s string) string { return escapeMarkdownV2(s, denyLinks, false) }

func denyLinks(string) bool { return false }

// escapeMarkdownV2 implements the EscapeMarkdownV2 family. allow == nil
// keeps every link (EscapeMarkdownV2); otherwise links are filtered through
// it, and dropBare also removes bare http(s) URLs it rejects.
func escapeMarkdownV2(s string, allow func(string) bool, dropBare bool) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "```"):
			end := strings.Index(s[i+3:], "```")
			if end < 0 {
				writeEscaped(&b, '`')
				i++
				continue
			}
			b.WriteString("```")
			writeCodeBody(&b, s[i+3:i+3+end])
			b.WriteString("```")
			i += 3 + end + 3

		case s[i] == '`':
			end := findInlineCodeEnd(s, i)
			if end < 0 {
				writeEscaped(&b, '`')
				i++
				continue
			}
			b.WriteByte('`')
			writeCodeBody(&b, s[i+1:end])
			b.WriteByte('`')
			i = end + 1

		case s[i] == '[':
			text, url, next, ok := matchLink(s, i)
			if !ok {
				writeEscaped(&b, '[')
				i++
				continue
			}
			if allow != nil && !allow(url) {
				// Keep what the reader saw, drop where it pointed.
				b.WriteString(escapeMarkdownV2(text, allow, dropBare))
				i = next
				continue
			}
			b.WriteByte('[')
			if allow != nil {
				b.WriteString(escapeMarkdownV2(text, denyLinks, false))
			} else {
				b.WriteString(EscapeMarkdownV2(text))
			}
			b.WriteString("](")
			writeURLBody(&b, url)
			b.WriteByte(')')
			i = next

		case dropBare && isBareURLStart(s, i):
			end := bareURLEnd(s, i)
			if !allow(s[i:end]) {
				i = end
				continue
			}
			for ; i < end; i++ {
				if c := s[i]; c < 128 && mdv2Specials[c] {
					b.WriteByte('\\')
				}
				b.WriteByte(s[i])
			}

		default:
			c := s[i]
			if c < 128 && mdv2Specials[c] {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// EscapeMarkdownV2Plain escapes every MarkdownV2 special character in `s`
// so the whole string renders as literal text. Unlike EscapeMarkdownV2 it
// preserves nothing: backticks, `[text](url)` links and fences all come out
// inert. Use it for text from untrusted upstreams (RSS feeds, scraped HTML)
// where a link or code span in the input must never become live markup in
// the channel. For LLM output derived from such text that should keep its
// formatting (the daily digest), use EscapeMarkdownV2Links with a link
// policy. Keep EscapeMarkdownV2 for prose from trusted upstreams, such as
// official release notes.
func EscapeMarkdownV2Plain(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 128 && mdv2Specials[c] {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// EscapeMarkdownV2Code escapes `s` for use inside an already-opened
// MarkdownV2 code span or fenced block. Only ` and \ need escaping there.
func EscapeMarkdownV2Code(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	writeCodeBody(&b, s)
	return b.String()
}

// EscapeMarkdownV2URL escapes `s` for use inside an already-opened link URL.
func EscapeMarkdownV2URL(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	writeURLBody(&b, s)
	return b.String()
}

// isBareURLStart reports whether a bare http:// or https:// URL starts at
// s[i]: the scheme matched case-insensitively, not preceded by a letter,
// digit, '_' or '/', and followed by at least one URL character.
func isBareURLStart(s string, i int) bool {
	if i > 0 {
		if p := s[i-1]; p == '/' || p == '_' || ('0' <= p && p <= '9') || ('a' <= p && p <= 'z') || ('A' <= p && p <= 'Z') {
			return false
		}
	}
	rest := s[i:]
	for _, scheme := range [...]string{"http://", "https://"} {
		if len(rest) > len(scheme) && strings.EqualFold(rest[:len(scheme)], scheme) &&
			!strings.ContainsRune(bareURLStop, rune(rest[len(scheme)])) {
			return true
		}
	}
	return false
}

// bareURLStop delimits an auto-detected link.
const bareURLStop = " \t\n\r\"'`<>"

// bareURLEnd returns the end of the bare URL starting at s[i]: it runs to
// whitespace, a quote, a backtick or angle bracket, minus trailing sentence
// punctuation, the way Telegram clients delimit an auto-detected link.
func bareURLEnd(s string, i int) int {
	end := i
	for end < len(s) && !strings.ContainsRune(bareURLStop, rune(s[end])) {
		end++
	}
	for end > i && strings.ContainsRune(".,;:!?)]}", rune(s[end-1])) {
		end--
	}
	return end
}

func writeEscaped(b *strings.Builder, c byte) {
	b.WriteByte('\\')
	b.WriteByte(c)
}

func writeCodeBody(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '`' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
}

func writeURLBody(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ')' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
}

func findInlineCodeEnd(s string, start int) int {
	for i := start + 1; i < len(s); i++ {
		switch s[i] {
		case '\n':
			return -1
		case '`':
			return i
		}
	}
	return -1
}

// matchLink tries to match a [text](url) link starting at s[start] == '['.
// It returns the text and URL slices (unescaped), the index just past the
// closing ')', and ok=true on success. Nested brackets are not supported.
func matchLink(s string, start int) (text, url string, end int, ok bool) {
	if start >= len(s) || s[start] != '[' {
		return "", "", 0, false
	}
	// find matching `]`
	i := start + 1
	depth := 1
	for i < len(s) && depth > 0 {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '[':
			depth++
		case ']':
			depth--
		case '\n':
			return "", "", 0, false
		}
		if depth == 0 {
			break
		}
		i++
	}
	if depth != 0 || i >= len(s) || s[i] != ']' {
		return "", "", 0, false
	}
	closeBracket := i
	if closeBracket+1 >= len(s) || s[closeBracket+1] != '(' {
		return "", "", 0, false
	}
	// find matching ')'
	j := closeBracket + 2
	for j < len(s) {
		switch s[j] {
		case '\\':
			j += 2
			continue
		case ')':
			text = s[start+1 : closeBracket]
			url = s[closeBracket+2 : j]
			if text == "" || url == "" {
				return "", "", 0, false
			}
			return text, url, j + 1, true
		case '\n':
			return "", "", 0, false
		}
		j++
	}
	return "", "", 0, false
}

// TruncateMarkdownV2 truncates `s` to at most `max` bytes. It prefers to
// cut at a paragraph boundary (blank line) and will never end inside a
// fenced code block or inline code span. When truncated, `suffix` is
// appended; the caller is responsible for ensuring `len(suffix) < max`.
func TruncateMarkdownV2(s, suffix string, max int) string {
	if len(s) <= max {
		return s
	}
	budget := max - len(suffix)
	if budget <= 0 {
		// caller passed a bad input; degrade gracefully by returning suffix.
		return suffix
	}
	cut := safeCut(s, budget)
	return s[:cut] + suffix
}

// safeCut returns the largest index <= limit (or the smallest > limit if
// none fits) such that s[:cut] is not inside an open code fence or span
// and — preferably — ends at a paragraph or line boundary.
//
// The algorithm walks s forward, tracking code-block boundaries. At each
// position outside a code block it records a "cut candidate". After the
// walk, it picks the best candidate: the largest <= limit, with a
// preference for paragraph boundaries ("\n\n") over plain line breaks
// over arbitrary rune boundaries.
func safeCut(s string, limit int) int {
	if limit >= len(s) {
		return len(s)
	}
	minCut := limit / 2

	var bestPara, bestLine, bestRune int
	bestPara, bestLine, bestRune = -1, -1, -1

	i := 0
	for i < len(s) {
		// record a candidate at i (end of s[:i]) when outside code.
		if i > 0 && i <= limit {
			if i >= 2 && s[i-1] == '\n' && s[i-2] == '\n' && i >= minCut {
				bestPara = i
			}
			if s[i-1] == '\n' && i >= minCut {
				bestLine = i
			}
			if utf8.RuneStart(s[i]) {
				bestRune = i
			}
		}
		switch {
		case strings.HasPrefix(s[i:], "```"):
			close := strings.Index(s[i+3:], "```")
			if close < 0 {
				// unclosed fence → cut before it.
				if bestPara >= 0 {
					return bestPara
				}
				if bestLine >= 0 {
					return bestLine
				}
				return safeRuneBoundary(s, min(i, limit))
			}
			i += 3 + close + 3
		case s[i] == '`':
			close := findInlineCodeEnd(s, i)
			if close < 0 {
				i++
				continue
			}
			i = close + 1
		default:
			i++
		}
	}

	if bestPara >= 0 {
		return bestPara
	}
	if bestLine >= 0 {
		return bestLine
	}
	if bestRune >= 0 {
		return bestRune
	}
	return safeRuneBoundary(s, limit)
}

func safeRuneBoundary(s string, i int) int {
	if i > len(s) {
		i = len(s)
	}
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}
