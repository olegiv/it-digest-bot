package gorelease

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// MaxSummaryBody leaves headroom for headers, links, and hashtags.
const MaxSummaryBody = 3200

// FormatRelease renders a MarkdownV2 announcement for a Go release.
func FormatRelease(version, summary, downloadURL, historyURL string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = FallbackSummary(version)
	}
	truncated := telegram.TruncateMarkdownV2(summary, "...", MaxSummaryBody)

	var sb strings.Builder
	sb.WriteString("🚀 *Go* `")
	sb.WriteString(telegram.EscapeMarkdownV2Code(version))
	sb.WriteString("` released\n\n")
	sb.WriteString(telegram.EscapeMarkdownV2(truncated))

	if len(summary) > MaxSummaryBody {
		sb.WriteString("\n\n📖 Full notes: ")
		sb.WriteString(telegram.EscapeMarkdownV2(historyURL))
	}

	fmt.Fprintf(&sb, "\n\n🔗 [Downloads](%s) · [Release history](%s)\n\n\\#Go \\#Golang",
		telegram.EscapeMarkdownV2URL(downloadURL),
		telegram.EscapeMarkdownV2URL(historyURL),
	)

	return sb.String()
}

// FallbackSummary is used if the release-history page is temporarily
// unavailable or its markup changes.
func FallbackSummary(version string) string {
	return "Official stable " + version + " release is available. See downloads and release history for details."
}

// DownloadURL returns the official go.dev downloads anchor for a version.
func DownloadURL(version string) string {
	return DefaultDownloadPageURL() + "#" + url.QueryEscape(version)
}

// ReleaseHistoryURL returns the official go.dev release-history anchor.
func ReleaseHistoryURL(version string) string {
	return DefaultReleaseHistoryURL + "#" + url.QueryEscape(version)
}

func DefaultDownloadPageURL() string {
	return "https://go.dev/dl/"
}
