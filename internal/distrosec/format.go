package distrosec

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Caps on escaped fields. Together they keep a post far below
// telegram.MaxMessageBytes; TestFormatNoticeEscapesAndBounds pins this.
const (
	maxTitleBytes    = 256
	maxExcerptBytes  = 700
	maxListedCVEs    = 8
	maxListedFixed   = 6
	maxListedPkgs    = 8
	maxFixedBytes    = 160
	ellipsis         = "…"
	updateUbuntuHint = "sudo apt update && sudo apt upgrade"
	updateDebianHint = "apt update && apt upgrade"
)

var esc = telegram.EscapeMarkdownV2Plain

// FormatNotice renders one advisory as a MarkdownV2 post. Every value from
// the feed is cleaned and plain-escaped; the only Markdown link is the
// advisory page built from the validated ID, and bare URLs in the text are
// removed unless they point at ubuntu.com or debian.org (plainText).
func FormatNotice(n *Notice) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s *%s security %s* — %s\n", severityEmoji(n), esc(n.Distro), kindWord(n), esc(n.ID))
	if n.Title != "" {
		fmt.Fprintf(&sb, "*%s*\n", capEscaped(esc(n.Title), maxTitleBytes))
	}
	if len(n.Packages) > 0 {
		fmt.Fprintf(&sb, "Packages: %s\n", codeList(n.Packages, maxListedPkgs))
	}
	if n.Remote {
		sb.WriteString("🌐 *Remotely exploitable* per the advisory text\n")
	}
	if len(n.Impacts) > 0 {
		fmt.Fprintf(&sb, "Impact: %s\n", esc(strings.Join(n.Impacts, ", ")))
	}
	if len(n.CVEs) > 0 {
		fmt.Fprintf(&sb, "CVE: %s\n", esc(listWithMore(n.CVEs, maxListedCVEs)))
	}
	if !n.Published.IsZero() {
		fmt.Fprintf(&sb, "Published: %s\n", esc(n.Published.UTC().Format("2006-01-02")))
	}

	if ex := excerpt(n.Paragraphs, maxExcerptBytes); ex != "" {
		fmt.Fprintf(&sb, "\n%s\n", ex)
	}

	sb.WriteString("\n")
	if len(n.Fixed) > 0 {
		fixed := n.Fixed
		if len(fixed) > maxListedFixed {
			fixed = fixed[:maxListedFixed]
		}
		parts := make([]string, 0, len(fixed))
		for _, f := range fixed {
			parts = append(parts, "`"+capEscaped(telegram.EscapeMarkdownV2Code(f), maxFixedBytes)+"`")
		}
		fmt.Fprintf(&sb, "🛠 Fixed in: %s\n", strings.Join(parts, ", "))
	}
	if n.ProOnly {
		sb.WriteString("⚠️ The fix is published for Ubuntu Pro only \\(ESM, FIPS or real\\-time kernel\\)\n")
	}
	hint := updateDebianHint
	if strings.HasPrefix(n.Distro, "Ubuntu") {
		hint = updateUbuntuHint
	}
	fmt.Fprintf(&sb, "Update: `%s`\n", telegram.EscapeMarkdownV2Code(hint))

	fmt.Fprintf(&sb, "\n🔗 [%s](%s)\n\n%s", esc(n.ID), telegram.EscapeMarkdownV2URL(n.Link), hashtags(n))
	return sb.String()
}

// excerpt joins as many whole escaped paragraphs as fit in limit bytes; the
// first one is always included, truncated if needed.
func excerpt(paras []string, limit int) string {
	var out []string
	used := 0
	for i, p := range paras {
		e := esc(p)
		if i == 0 {
			e = capEscaped(e, limit)
		} else if used+2+len(e) > limit {
			break
		}
		out = append(out, e)
		used += len(e) + 2
	}
	return strings.Join(out, "\n\n")
}

func kindWord(n *Notice) string {
	if n.Regression {
		return "regression fix"
	}
	if strings.HasPrefix(n.ID, "DSA-") {
		return "advisory"
	}
	return "notice"
}

// severityEmoji ranks what the text says: regression fixes ♻️, remote
// code execution 🔴, anything remote, code execution or privilege
// escalation 🟠, the rest 🟡.
func severityEmoji(n *Notice) string {
	codeExec := slices.Contains(n.Impacts, "code execution")
	switch {
	case n.Regression:
		return "♻️"
	case n.Remote && codeExec:
		return "🔴"
	case n.Remote || codeExec || slices.Contains(n.Impacts, "privilege escalation"):
		return "🟠"
	default:
		return "🟡"
	}
}

func hashtags(n *Notice) string {
	distro := "Debian"
	if strings.HasPrefix(n.Distro, "Ubuntu") {
		distro = "Ubuntu"
	}
	return "\\#" + distro + " \\#Security"
}

func codeList(items []string, limit int) string {
	shown := items
	if len(shown) > limit {
		shown = shown[:limit]
	}
	parts := make([]string, 0, len(shown)+1)
	for _, it := range shown {
		parts = append(parts, "`"+telegram.EscapeMarkdownV2Code(it)+"`")
	}
	if extra := len(items) - len(shown); extra > 0 {
		parts = append(parts, esc(fmt.Sprintf("+%d more", extra)))
	}
	return strings.Join(parts, ", ")
}

func listWithMore(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(" (+%d more)", len(items)-limit)
}

// capEscaped truncates an already-escaped MarkdownV2 fragment to at most
// limit bytes, appending "…", on a rune boundary and without leaving a
// dangling backslash.
func capEscaped(escaped string, limit int) string {
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
