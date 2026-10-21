package applerelease

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRelease(t *testing.T) {
	t.Parallel()
	r := &Release{
		Platform: PlatformIOS,
		Version:  "27.0.1",
		Build:    "24A446",
		Link:     "https://developer.apple.com/news/releases/?id=09282026b",
		NotesURL: "https://developer.apple.com/go/?id=ios-27_0_1-rn",
	}
	want := "🍎 *iOS* `27.0.1` released\n" +
		"Build `24A446`\n" +
		"\n🔗 [Release notes](https://developer.apple.com/go/?id=ios-27_0_1-rn) · " +
		"[Security content](https://support.apple.com/en-us/100100) · " +
		"[Announcement](https://developer.apple.com/news/releases/?id=09282026b)\n\n" +
		"\\#iOS \\#Apple"
	if got := FormatRelease(r, false); got != want {
		t.Errorf("FormatRelease =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatReleaseLegacyMacOS(t *testing.T) {
	t.Parallel()
	r := &Release{
		Platform: PlatformMacOS,
		Name:     "Tahoe",
		Version:  "26.6.2",
		Build:    "25G83",
		Link:     "https://developer.apple.com/news/releases/?id=08172026b",
	}
	got := FormatRelease(r, true)
	for _, want := range []string{
		"🍎 *macOS Tahoe* `26.6.2` released\n",
		"🔒 Update for the older macOS 26 branch\n",
		"\n🔗 [Security content](https://support.apple.com/en-us/100100) · [Announcement]",
		"\\#macOS \\#Apple",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Release notes") {
		t.Errorf("no NotesURL, but release-notes link rendered:\n%s", got)
	}
}

func TestFormatSeedNotice(t *testing.T) {
	t.Parallel()
	newest := &Release{Platform: PlatformIOS, Version: "27.0.1", Published: time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)}
	want := "🍎 *iOS releases* are now tracked here\\.\n\n" +
		"Recorded 3 stable releases currently listed on developer\\.apple\\.com as the baseline \\(latest: `27.0.1`, 2026\\-09\\-28\\)\\. " +
		"New stable iOS releases will be posted as they ship\\.\n\n" +
		"🔗 [Apple Developer releases](https://developer.apple.com/news/releases/)\n\n\\#iOS \\#Apple"
	if got := FormatSeedNotice(PlatformIOS, 3, newest); got != want {
		t.Errorf("FormatSeedNotice =\n%s\nwant\n%s", got, want)
	}

	got := FormatSeedNotice(PlatformMacOS, 1, nil)
	if !strings.Contains(got, "Recorded 1 stable release currently") || strings.Contains(got, "latest") {
		t.Errorf("FormatSeedNotice(nil newest) =\n%s", got)
	}
}

// TestFormatReleaseEscapesLinkBreakout checks that a link carrying ")" and
// "](" cannot close the MarkdownV2 link early and inject a second target.
func TestFormatReleaseEscapesLinkBreakout(t *testing.T) {
	t.Parallel()
	r := &Release{
		Platform: PlatformIOS,
		Version:  "27.0.1",
		Link:     "https://developer.apple.com/news/releases/?id=1",
		NotesURL: "https://developer.apple.com/go/?id=a)](https://evil.example",
	}
	got := FormatRelease(r, false)
	if !strings.Contains(got, `[Release notes](https://developer.apple.com/go/?id=a\)](https://evil.example)`) {
		t.Errorf("link not escaped:\n%s", got)
	}
}
