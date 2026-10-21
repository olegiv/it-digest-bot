package applerelease

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// esc is the escaper for every feed-derived string. The plain variant keeps
// feed content from turning into live markup.
var esc = telegram.EscapeMarkdownV2Plain

// FormatRelease renders one stable release as a MarkdownV2 post. legacy marks
// an update to an older major branch (e.g. iOS 18.7.10 while iOS 27 is
// current).
func FormatRelease(r *Release, legacy bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🍎 *%s* `%s` released\n", esc(r.DisplayName()), telegram.EscapeMarkdownV2Code(r.Version))
	if r.Build != "" {
		fmt.Fprintf(&sb, "Build `%s`\n", telegram.EscapeMarkdownV2Code(r.Build))
	}
	if legacy {
		fmt.Fprintf(&sb, "🔒 Update for the older %s branch\n",
			esc(string(r.Platform)+" "+strconv.Itoa(r.Major())))
	}

	links := make([]string, 0, 3)
	if r.NotesURL != "" {
		links = append(links, mdLink("Release notes", r.NotesURL))
	}
	links = append(links,
		mdLink("Security content", SecurityReleasesURL),
		mdLink("Announcement", r.Link),
	)
	fmt.Fprintf(&sb, "\n🔗 %s\n\n%s", strings.Join(links, " · "), hashtags(r.Platform))
	return sb.String()
}

// FormatSeedNotice is the one-off post for a platform's first run: count
// releases currently in the feed were recorded as the baseline.
func FormatSeedNotice(p Platform, count int, newest *Release) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🍎 *%s releases* are now tracked here\\.\n\n", esc(string(p)))
	fmt.Fprintf(&sb, "Recorded %s currently listed on developer\\.apple\\.com as the baseline", esc(plural(count, "stable release", "stable releases")))
	if newest != nil {
		fmt.Fprintf(&sb, " \\(latest: `%s`", telegram.EscapeMarkdownV2Code(newest.Version))
		if !newest.Published.IsZero() {
			fmt.Fprintf(&sb, ", %s", esc(newest.Published.UTC().Format("2006-01-02")))
		}
		sb.WriteString("\\)")
	}
	fmt.Fprintf(&sb, "\\. New stable %s releases will be posted as they ship\\.\n\n", esc(string(p)))
	fmt.Fprintf(&sb, "🔗 %s\n\n%s", mdLink("Apple Developer releases", ReleasesPageURL), hashtags(p))
	return sb.String()
}

func mdLink(text, target string) string {
	return "[" + esc(text) + "](" + telegram.EscapeMarkdownV2URL(target) + ")"
}

func hashtags(p Platform) string {
	return "\\#" + esc(string(p)) + " \\#Apple"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
