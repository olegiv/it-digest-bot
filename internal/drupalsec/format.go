package drupalsec

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

const (
	// MaxDescriptionBytes caps the description excerpt in a post.
	MaxDescriptionBytes = 700
	// MaxSolutionLines caps the solution bullet list in a post.
	MaxSolutionLines = 6
	// MaxSolutionLineBytes caps each escaped solution line, so one long
	// paragraph degrades to "…" instead of pushing the whole post over the
	// limit and losing the entire Solution block to the size fallback.
	MaxSolutionLineBytes = 400
	// MaxFieldBytes caps each escaped header field (ID, title, project,
	// risk, vulnerability, affected versions). Together with MaxCVEBytes and
	// MaxLinkBytes it bounds the header at about 3.2 KiB (seven MaxFieldBytes
	// fields, one MaxCVEBytes list, and a MaxLinkBytes link that URL
	// escaping can double), so even a description-less, solution-less post
	// fits telegram.MaxMessageBytes. TestFormatAdvisoryBoundsHeaderFields
	// pins this.
	MaxFieldBytes = 256
	// MaxCVEBytes caps the escaped, comma-joined CVE list.
	MaxCVEBytes = 512
	// MaxLinkBytes caps the unescaped item link (drupal.org's are ~45
	// bytes). ParseFeed skips longer links with a warning; advisoryLink keeps
	// a SecurityPageURL fallback only as defence in depth for hand-built
	// advisories.
	MaxLinkBytes = 512

	hashtags = "\\#Drupal \\#Security"
	ellipsis = "…"
)

// FormatAdvisory renders one advisory as a MarkdownV2 post. Dynamic values
// are escaped individually and every header field is capped (MaxFieldBytes,
// MaxCVEBytes, MaxLinkBytes), so the result always fits
// telegram.MaxMessageBytes: long descriptions and solutions are dropped
// first, and the header and link can never grow past the limit on their own.
func FormatAdvisory(a *Advisory) string {
	text := render(a, true, true)
	if len(text) > telegram.MaxMessageBytes {
		text = render(a, false, true)
	}
	if len(text) > telegram.MaxMessageBytes {
		text = render(a, false, false)
	}
	return text
}

// esc is the escaper for every feed-derived string. It must be the plain
// variant: drupal.org content is untrusted input, and the construct-preserving
// telegram.EscapeMarkdownV2 would let a `[text](url)` or backtick span in an
// advisory become live markup in the channel.
var esc = telegram.EscapeMarkdownV2Plain

func render(a *Advisory, withDescription, withSolution bool) string {
	var sb strings.Builder

	id := a.ID
	if id == "" {
		id = a.Version()
	}
	if a.Kind == KindPSA {
		fmt.Fprintf(&sb, "📢 *Drupal PSA* — %s\n", field(id))
		fmt.Fprintf(&sb, "*%s*\n", field(psaTitle(a)))
	} else {
		fmt.Fprintf(&sb, "%s *Drupal security advisory* — %s\n", riskEmoji(a.RiskLabel), field(id))
		writeProjectLine(&sb, a)
		sb.WriteString("\n")
		if a.RiskLabel != "" {
			fmt.Fprintf(&sb, "Risk: *%s*", field(a.RiskLabel))
			if a.RiskScore != "" {
				fmt.Fprintf(&sb, " %s", field(a.RiskScore))
			}
			sb.WriteString("\n")
		}
		if a.Vulnerability != "" {
			fmt.Fprintf(&sb, "Vulnerability: %s\n", field(a.Vulnerability))
		}
		if a.AffectedVersions != "" {
			fmt.Fprintf(&sb, "Affected versions: `%s`\n", codeField(a.AffectedVersions))
		}
		if len(a.CVEs) > 0 {
			fmt.Fprintf(&sb, "CVE: %s\n", capField(esc(strings.Join(a.CVEs, ", ")), MaxCVEBytes))
		}
	}
	if !a.Published.IsZero() {
		fmt.Fprintf(&sb, "Published: %s\n", esc(a.Published.UTC().Format("2006-01-02 15:04 UTC")))
	}

	if withDescription && a.Description != "" {
		fmt.Fprintf(&sb, "\n%s\n", truncateEscaped(esc(a.Description), MaxDescriptionBytes))
	}
	if withSolution {
		if lines := solutionLines(a.Solution, MaxSolutionLines); len(lines) > 0 {
			sb.WriteString("\n🛠 *Solution*\n")
			for _, line := range lines {
				fmt.Fprintf(&sb, "%s\n", capField(esc(line), MaxSolutionLineBytes))
			}
		}
	}

	fmt.Fprintf(&sb, "\n🔗 [Advisory](%s)\n\n%s", telegram.EscapeMarkdownV2URL(advisoryLink(a)), hashtags)
	return sb.String()
}

// field escapes a feed-derived header value and caps it at MaxFieldBytes.
func field(s string) string { return capField(esc(s), MaxFieldBytes) }

// codeField is field for values rendered inside a code span.
func codeField(s string) string { return capField(telegram.EscapeMarkdownV2Code(s), MaxFieldBytes) }

// advisoryLink returns the item link, or the security landing page when the
// link is missing or longer than MaxLinkBytes. ParseFeed already rejects both
// cases, so for parsed advisories this is unreachable defence in depth.
func advisoryLink(a *Advisory) string {
	if a.Link == "" || len(a.Link) > MaxLinkBytes {
		return SecurityPageURL
	}
	return a.Link
}

// capField truncates an already-escaped MarkdownV2 fragment to at most limit
// bytes, appending "…". It cuts on a rune boundary and never leaves a
// dangling backslash, which would otherwise escape whatever the template
// places after the field.
func capField(escaped string, limit int) string {
	if len(escaped) <= limit {
		return escaped
	}
	cut := limit - len(ellipsis)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && cut < len(escaped) && !utf8.RuneStart(escaped[cut]) {
		cut--
	}
	return trimDanglingBackslash(escaped[:cut]) + ellipsis
}

// truncateEscaped shortens an already-escaped MarkdownV2 fragment to at most
// limit bytes, preferring a paragraph or line boundary like
// telegram.TruncateMarkdownV2 but, unlike it, never cutting between an
// escape backslash and the character it escapes. A lone "\" before "…"
// makes Telegram reject the whole message.
func truncateEscaped(escaped string, limit int) string {
	if len(escaped) <= limit {
		return escaped
	}
	cut := telegram.TruncateMarkdownV2(escaped, "", limit-len(ellipsis))
	return trimDanglingBackslash(cut) + ellipsis
}

// trimDanglingBackslash drops the final byte of s when s ends in an odd run
// of backslashes, i.e. when the last backslash escapes a character that was
// cut off.
func trimDanglingBackslash(s string) string {
	trailing := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		trailing++
	}
	if trailing%2 == 1 {
		return s[:len(s)-1]
	}
	return s
}

// FormatSeedNotice renders the one-off post made when the source records the
// feed's existing history instead of announcing every historical item.
func FormatSeedNotice(count int, newest *Advisory) string {
	var sb strings.Builder
	sb.WriteString("🛡️ *Drupal security advisories* are now tracked here\\.\n\n")
	fmt.Fprintf(&sb, "Recorded %s currently listed on drupal\\.org as the baseline", esc(plural(count, "advisory", "advisories")))
	if newest != nil && newest.ID != "" {
		fmt.Fprintf(&sb, " \\(newest: %s", field(newest.ID))
		if !newest.Published.IsZero() {
			fmt.Fprintf(&sb, ", %s", esc(newest.Published.UTC().Format("2006-01-02")))
		}
		sb.WriteString("\\)")
	}
	sb.WriteString("\\. New core, contributed\\-project and PSA advisories will be posted as they are published\\.\n\n")
	fmt.Fprintf(&sb, "🔗 [drupal\\.org/security](%s)\n\n%s", telegram.EscapeMarkdownV2URL(SecurityPageURL), hashtags)
	return sb.String()
}

func writeProjectLine(sb *strings.Builder, a *Advisory) {
	name := a.ProjectName
	if name == "" {
		name = a.Title
	}
	fmt.Fprintf(sb, "*%s*", field(name))
	if a.ProjectMachineName != "" && a.ProjectMachineName != name {
		fmt.Fprintf(sb, " \\(`%s`\\)", codeField(a.ProjectMachineName))
	}
	sb.WriteString("\n")
}

// psaTitle strips the trailing " - PSA-…" identifier, which the header
// already shows.
func psaTitle(a *Advisory) string {
	if t, ok := strings.CutSuffix(a.Title, " - "+a.ID); ok && a.ID != "" {
		return t
	}
	return a.Title
}

func riskEmoji(label string) string {
	switch strings.ToLower(label) {
	case "highly critical", "critical":
		return "🔴"
	case "moderately critical":
		return "🟠"
	case "less critical":
		return "🟡"
	case "not critical":
		return "⚪"
	default:
		return "🛡️"
	}
}

// solutionLines returns at most maxLines non-empty lines of the solution,
// with a trailing "… (N more)" marker when lines were dropped.
func solutionLines(solution string, maxLines int) []string {
	var lines []string
	for line := range strings.SplitSeq(solution, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		dropped := len(lines) - maxLines
		lines = append(lines[:maxLines], fmt.Sprintf("… (%s)", plural(dropped, "more line", "more lines")))
	}
	return lines
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, pluralForm)
}
