package drupalsec

import (
	"fmt"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

const (
	// MaxDescriptionBytes caps the description excerpt in a post.
	MaxDescriptionBytes = 700
	// MaxSolutionLines caps the solution bullet list in a post.
	MaxSolutionLines = 6

	hashtags = "\\#Drupal \\#Security"
)

// FormatAdvisory renders one advisory as a MarkdownV2 post. Dynamic values
// are escaped individually; the result is guaranteed to fit
// telegram.MaxMessageBytes (long descriptions and solutions are dropped
// before the header and link ever would be).
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
		fmt.Fprintf(&sb, "📢 *Drupal PSA* — %s\n", esc(id))
		fmt.Fprintf(&sb, "*%s*\n", esc(psaTitle(a)))
	} else {
		fmt.Fprintf(&sb, "%s *Drupal security advisory* — %s\n", riskEmoji(a.RiskLabel), esc(id))
		writeProjectLine(&sb, a)
		sb.WriteString("\n")
		if a.RiskLabel != "" {
			fmt.Fprintf(&sb, "Risk: *%s*", esc(a.RiskLabel))
			if a.RiskScore != "" {
				fmt.Fprintf(&sb, " %s", esc(a.RiskScore))
			}
			sb.WriteString("\n")
		}
		if a.Vulnerability != "" {
			fmt.Fprintf(&sb, "Vulnerability: %s\n", esc(a.Vulnerability))
		}
		if a.AffectedVersions != "" {
			fmt.Fprintf(&sb, "Affected versions: `%s`\n", telegram.EscapeMarkdownV2Code(a.AffectedVersions))
		}
		if len(a.CVEs) > 0 {
			fmt.Fprintf(&sb, "CVE: %s\n", esc(strings.Join(a.CVEs, ", ")))
		}
	}
	if !a.Published.IsZero() {
		fmt.Fprintf(&sb, "Published: %s\n", esc(a.Published.UTC().Format("2006-01-02 15:04 UTC")))
	}

	if withDescription && a.Description != "" {
		desc := telegram.TruncateMarkdownV2(esc(a.Description), "…", MaxDescriptionBytes)
		fmt.Fprintf(&sb, "\n%s\n", desc)
	}
	if withSolution {
		if lines := solutionLines(a.Solution, MaxSolutionLines); len(lines) > 0 {
			sb.WriteString("\n🛠 *Solution*\n")
			for _, line := range lines {
				fmt.Fprintf(&sb, "%s\n", esc(line))
			}
		}
	}

	fmt.Fprintf(&sb, "\n🔗 [Advisory](%s)\n\n%s", telegram.EscapeMarkdownV2URL(a.Link), hashtags)
	return sb.String()
}

// FormatSeedNotice renders the one-off post made when the source records the
// feed's existing history instead of announcing every historical item.
func FormatSeedNotice(count int, newest *Advisory) string {
	var sb strings.Builder
	sb.WriteString("🛡️ *Drupal security advisories* are now tracked here\\.\n\n")
	fmt.Fprintf(&sb, "Recorded %s currently listed on drupal\\.org as the baseline", esc(plural(count, "advisory", "advisories")))
	if newest != nil && newest.ID != "" {
		fmt.Fprintf(&sb, " \\(newest: %s", esc(newest.ID))
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
	fmt.Fprintf(sb, "*%s*", esc(name))
	if a.ProjectMachineName != "" && a.ProjectMachineName != name {
		fmt.Fprintf(sb, " \\(`%s`\\)", telegram.EscapeMarkdownV2Code(a.ProjectMachineName))
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
