package phpsec

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

const (
	// MaxFieldBytes caps each escaped header field (ID, summary, CVE,
	// package and version ranges).
	MaxFieldBytes = 320
	// MaxExcerptBytes caps the escaped description excerpt.
	MaxExcerptBytes = 700
	// MaxVulnerabilityLines caps the affected/patched list in a post.
	MaxVulnerabilityLines = 4
	// MaxRangeBytes caps each escaped package name and version range in
	// the affected/patched list. With MaxFieldBytes it bounds the post
	// without the excerpt well below telegram.MaxMessageBytes;
	// TestFormatAdvisoryBoundsSize pins this.
	MaxRangeBytes = 160
	// MaxCWEs caps the CWE list in a post.
	MaxCWEs = 4

	hashtags = "\\#PHP \\#Security"
	ellipsis = "…"
)

// esc is the escaper for every API-derived string. It must be the plain
// variant: advisory text is untrusted input, and the construct-preserving
// telegram.EscapeMarkdownV2 would let a `[text](url)` or backtick span in a
// description become live markup in the channel.
var esc = telegram.EscapeMarkdownV2Plain

// FormatAdvisory renders one advisory as a MarkdownV2 post. Every dynamic
// value is escaped and capped, so the result always fits
// telegram.MaxMessageBytes (the excerpt is dropped first if it would not).
func FormatAdvisory(a *Advisory) string {
	text := render(a, true)
	if len(text) > telegram.MaxMessageBytes {
		text = render(a, false)
	}
	return text
}

func render(a *Advisory, withExcerpt bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s *PHP security advisory* — %s\n", severityEmoji(a.Severity), field(a.ID))
	if a.Summary != "" {
		fmt.Fprintf(&sb, "*%s*\n", field(a.Summary))
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Severity: *%s*", esc(severityLabel(a.Severity)))
	if a.Score > 0 {
		fmt.Fprintf(&sb, " %s", esc(strconv.FormatFloat(a.Score, 'f', 1, 64)))
	}
	sb.WriteString("\n")
	if a.CVE != "" {
		fmt.Fprintf(&sb, "CVE: %s\n", field(a.CVE))
	}
	if len(a.CWEs) > 0 {
		cwes := a.CWEs
		if len(cwes) > MaxCWEs {
			cwes = cwes[:MaxCWEs]
		}
		fmt.Fprintf(&sb, "CWE: %s\n", esc(strings.Join(cwes, ", ")))
	}
	if !a.Published.IsZero() {
		fmt.Fprintf(&sb, "Published: %s\n", esc(a.Published.UTC().Format("2006-01-02 15:04 UTC")))
	}

	if lines := vulnerabilityLines(a.Vulnerabilities); len(lines) > 0 {
		sb.WriteString("\n🛠 *Affected / fixed*\n")
		for _, line := range lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	if withExcerpt {
		if ex := Excerpt(a.Description); ex != "" {
			fmt.Fprintf(&sb, "\n%s\n", capField(esc(ex), MaxExcerptBytes))
		}
	}

	fmt.Fprintf(&sb, "\n🔗 [Advisory](%s) · [All PHP advisories](%s)\n\n%s",
		telegram.EscapeMarkdownV2URL(a.Link),
		telegram.EscapeMarkdownV2URL(AdvisoriesPageURL),
		hashtags)
	return sb.String()
}

func vulnerabilityLines(vulns []Vulnerability) []string {
	lines := make([]string, 0, min(len(vulns), MaxVulnerabilityLines)+1)
	for i, v := range vulns {
		if i == MaxVulnerabilityLines {
			lines = append(lines, esc(fmt.Sprintf("… (%d more)", len(vulns)-i)))
			break
		}
		var parts []string
		if v.Vulnerable != "" {
			parts = append(parts, "affected `"+rangeField(v.Vulnerable)+"`")
		}
		if v.Patched != "" {
			parts = append(parts, "fixed in `"+rangeField(v.Patched)+"`")
		}
		if len(parts) == 0 {
			continue
		}
		line := "• "
		if v.Package != "" {
			line += capField(esc(v.Package), MaxRangeBytes) + ": "
		}
		lines = append(lines, line+strings.Join(parts, ", "))
	}
	return lines
}

// Excerpt returns the first prose paragraph of a GitHub advisory
// description: markdown headings ("### Summary"), fenced code and empty lines
// are skipped, inline emphasis and code markers are dropped, control and
// Unicode format characters are removed (see telegram.CleanText) and whitespace is
// collapsed. The result is plain text that still needs escaping.
func Excerpt(desc string) string {
	var para []string
	inFence := false
	for line := range strings.SplitSeq(strings.ReplaceAll(desc, "\r\n", "\n"), "\n") {
		trimmed := telegram.CleanText(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			if len(para) > 0 {
				break
			}
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, trimmed)
	}
	return strings.NewReplacer("**", "", "__", "", "`", "").Replace(strings.Join(para, " "))
}

// FormatSeedNotice renders the one-off post made when the source records the
// existing advisories instead of announcing each historical one.
func FormatSeedNotice(count int, newest *Advisory) string {
	var sb strings.Builder
	sb.WriteString("🛡️ *PHP security advisories* are now tracked here\\.\n\n")
	fmt.Fprintf(&sb, "Recorded %s published for php\\-src as the baseline", esc(plural(count, "advisory", "advisories")))
	if newest != nil {
		fmt.Fprintf(&sb, " \\(newest: %s", field(newest.ID))
		if !newest.Published.IsZero() {
			fmt.Fprintf(&sb, ", %s", esc(newest.Published.UTC().Format("2006-01-02")))
		}
		sb.WriteString("\\)")
	}
	sb.WriteString("\\. New advisories will be posted as they are published\\.\n\n")
	fmt.Fprintf(&sb, "🔗 [PHP security advisories](%s)\n\n%s", telegram.EscapeMarkdownV2URL(AdvisoriesPageURL), hashtags)
	return sb.String()
}

func severityEmoji(s string) string {
	switch s {
	case SeverityCritical:
		return "🔴"
	case SeverityHigh:
		return "🟠"
	case SeverityMedium:
		return "🟡"
	case SeverityLow:
		return "⚪"
	default:
		return "🛡️"
	}
}

func severityLabel(s string) string {
	if s == "" {
		s = SeverityUnknown
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func field(s string) string { return capField(esc(s), MaxFieldBytes) }

func rangeField(s string) string { return capField(telegram.EscapeMarkdownV2Code(s), MaxRangeBytes) }

// capField truncates an already-escaped MarkdownV2 fragment to at most limit
// bytes, appending "…". It cuts on a rune boundary and never leaves a
// dangling backslash, which would escape whatever the template places next.
func capField(escaped string, limit int) string {
	if len(escaped) <= limit {
		return escaped
	}
	cut := limit - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(escaped[cut]) {
		cut--
	}
	s := escaped[:cut]
	trailing := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		trailing++
	}
	if trailing%2 == 1 {
		s = s[:len(s)-1]
	}
	return s + ellipsis
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
