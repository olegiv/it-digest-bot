package claudecode

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// MaxNotesBody is the maximum escaped-byte length allocated to the release
// notes body inside a single Telegram message. Leaves headroom for the
// header + footer lines under Telegram's 4096 hard cap.
const MaxNotesBody = 3500

// FormatRelease renders the MarkdownV2 announcement from the template in
// the spec. `version` and `releaseURL` are trusted dynamic values,
// `notesBody` is the raw GitHub/CHANGELOG markdown (possibly with links,
// code spans, issue refs) which the escaper handles.
func FormatRelease(version, notesBody, releaseURL, npmURL string) string {
	truncated := telegram.TruncateMarkdownV2(notesBody, "…", MaxNotesBody)

	var sb strings.Builder
	sb.WriteString("🚀 *Claude Code* `")
	sb.WriteString(telegram.EscapeMarkdownV2Code(version))
	sb.WriteString("` released\n\n")
	sb.WriteString(telegram.EscapeMarkdownV2(truncated))

	if len(notesBody) > MaxNotesBody {
		sb.WriteString("\n\n📖 Full notes: ")
		sb.WriteString(telegram.EscapeMarkdownV2(releaseURL))
	}

	fmt.Fprintf(&sb, "\n\n🔗 [npm](%s) · [GitHub](%s)\n\n\\#ClaudeCode \\#AI",
		telegram.EscapeMarkdownV2URL(npmURL),
		telegram.EscapeMarkdownV2URL(releaseURL),
	)

	return sb.String()
}

// NPMPackageURL returns the public npmjs.com page URL for a package.
// The package name is path-escaped to handle scoped packages and any
// exotic characters that might appear in a misconfigured package name.
func NPMPackageURL(pkg string) string {
	return "https://www.npmjs.com/package/" + url.PathEscape(pkg)
}
