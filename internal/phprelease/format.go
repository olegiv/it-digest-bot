package phprelease

import (
	"fmt"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

const hashtagPHP = "\\#PHP"

// FormatRelease renders one release as a MarkdownV2 post. current marks the
// newest active branch; older branches are labelled so readers can tell a
// maintenance update from a release of the current branch.
func FormatRelease(r *Release, current bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🐘 *PHP* `%s` released\n", telegram.EscapeMarkdownV2Code(r.Version))
	switch {
	case r.IsNewBranch():
		fmt.Fprintf(&sb, "🎉 First release of the new %s branch\n", esc("PHP "+r.Branch))
	case current:
		fmt.Fprintf(&sb, "Current %s branch\n", esc(r.Branch))
	default:
		fmt.Fprintf(&sb, "Update for the older %s branch\n", esc(r.Branch))
	}
	if r.Security {
		sb.WriteString("🔒 *Security release* — upgrading is strongly encouraged\n")
	}
	if !r.Date.IsZero() {
		fmt.Fprintf(&sb, "Released: %s\n", esc(r.Date.Format("2006-01-02")))
	}

	links := make([]string, 0, 3)
	if u := r.AnnouncementURL(); u != "" {
		links = append(links, mdLink("Announcement", u))
	}
	links = append(links, mdLink("ChangeLog", r.ChangeLogURL()), mdLink("Downloads", DownloadsURL))
	fmt.Fprintf(&sb, "\n🔗 %s\n\n%s", strings.Join(links, " · "), hashtags(r.Security))
	return sb.String()
}

// FormatSeedNotice is the one-off post for the source's first run: the
// current tip of every active branch is recorded as the baseline.
func FormatSeedNotice(releases []*Release) string {
	var sb strings.Builder
	sb.WriteString("🐘 *PHP releases* are now tracked here\\.\n\n")
	fmt.Fprintf(&sb, "Recorded the latest release of %s as the baseline", esc(plural(len(releases), "supported branch", "supported branches")))
	if len(releases) > 0 {
		versions := make([]string, 0, len(releases))
		for _, r := range releases {
			versions = append(versions, "`"+telegram.EscapeMarkdownV2Code(r.Version)+"`")
		}
		fmt.Fprintf(&sb, ": %s", strings.Join(versions, ", "))
	}
	sb.WriteString("\\. New PHP releases, including security releases, will be posted as they ship\\.\n\n")
	fmt.Fprintf(&sb, "🔗 %s\n\n%s", mdLink("Supported versions", SupportedVersionsURL), hashtagPHP)
	return sb.String()
}

var esc = telegram.EscapeMarkdownV2Plain

func mdLink(text, target string) string {
	return "[" + esc(text) + "](" + telegram.EscapeMarkdownV2URL(target) + ")"
}

func hashtags(security bool) string {
	if security {
		return hashtagPHP + " \\#Security"
	}
	return hashtagPHP
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
