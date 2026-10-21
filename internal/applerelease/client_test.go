package applerelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

func item(title, id, pubDate, notesHref string) string {
	content := `<ul class=links-stacked><li><a href=/download/os class=more>View downloads</a></li>`
	if notesHref != "" {
		content += `<li><a href=` + notesHref + ` class=more>View release notes</a></li>`
	}
	content += `</ul>`
	return `<item>
<title>` + title + `</title>
<link>https://developer.apple.com/news/releases/?id=` + id + `</link>
<guid>https://developer.apple.com/news/releases/?id=` + id + `</guid>
<description>View downloadsView release notes</description>
<pubDate>` + pubDate + `</pubDate>
<content:encoded><![CDATA[` + content + `]]></content:encoded>
</item>
`
}

func feedWith(items ...string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
<title>Releases - Apple Developer</title>
<link>https://developer.apple.com/news/</link>
<description>Apple Developer News and Updates feed provided by Apple, Inc.</description>
` + strings.Join(items, "") + `</channel>
</rss>`)
}

// liveItems are titles and dates taken from the live feed (2026-10-21),
// newest first, plus a marketing-name macOS title.
var liveItems = []string{
	item("Xcode 27.2 RC (27B48)", "10192026g", "Mon, 19 Oct 2026 10:00:00 PDT", "/go/?id=xcode-27_2-sdk-rn"),
	item("iOS 27.2 RC (24B115)", "10192026a", "Mon, 19 Oct 2026 10:00:00 PDT", "/go/?id=ios-27_2-rn"),
	item("macOS 27.2 RC (26B118)", "10192026c", "Mon, 19 Oct 2026 10:00:00 PDT", "/go/?id=macos-27_2-rn"),
	item("tvOS 27.2 beta 3 (24K5103f)", "10052026a", "Mon, 05 Oct 2026 11:00:00 PDT", ""),
	item("iOS 27.0.1 (24A446)", "09282026b", "Mon, 28 Sep 2026 10:00:00 PDT", "/go/?id=ios-27_0_1-rn"),
	item("iPadOS 27.0.1 (24A446)", "09282026c", "Mon, 28 Sep 2026 10:00:00 PDT", ""),
	item("macOS 27.0.1 (26A434)", "09282026d", "Mon, 28 Sep 2026 10:00:00 PDT", "https://developer.apple.com/go/?id=macos-27_0_1-rn"),
	item("iOS 26.6.2 (23G90)", "09082026a", "Tue, 08 Sep 2026 10:00:00 PDT", ""),
	item("macOS Tahoe 26.6.2 (25G83)", "08172026b", "Mon, 17 Aug 2026 10:00:00 PDT", "https://evil.example/notes"),
	item("iOS 18.7.10 (22H374)", "08172026c", "Mon, 17 Aug 2026 10:00:00 PDT", ""),
}

func TestParseFeedKeepsStableIOSAndMacOS(t *testing.T) {
	t.Parallel()
	releases, warnings, err := ParseFeed(feedWith(liveItems...))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	want := []struct {
		platform       Platform
		name, version  string
		build, notes   string
		displayName    string
		major          int
		publishedMonth time.Month
	}{
		{PlatformIOS, "", "27.0.1", "24A446", "https://developer.apple.com/go/?id=ios-27_0_1-rn", "iOS", 27, time.September},
		{PlatformMacOS, "", "27.0.1", "26A434", "https://developer.apple.com/go/?id=macos-27_0_1-rn", "macOS", 27, time.September},
		{PlatformIOS, "", "26.6.2", "23G90", "", "iOS", 26, time.September},
		{PlatformMacOS, "Tahoe", "26.6.2", "25G83", "", "macOS Tahoe", 26, time.August},
		{PlatformIOS, "", "18.7.10", "22H374", "", "iOS", 18, time.August},
	}
	if len(releases) != len(want) {
		t.Fatalf("releases = %d (%+v), want %d", len(releases), releases, len(want))
	}
	for i, w := range want {
		r := releases[i]
		if r.Platform != w.platform || r.Name != w.name || r.Version != w.version || r.Build != w.build {
			t.Errorf("[%d] = %s/%q/%s/%s, want %s/%q/%s/%s", i, r.Platform, r.Name, r.Version, r.Build, w.platform, w.name, w.version, w.build)
		}
		if r.NotesURL != w.notes {
			t.Errorf("[%d] NotesURL = %q, want %q", i, r.NotesURL, w.notes)
		}
		if r.DisplayName() != w.displayName || r.Major() != w.major {
			t.Errorf("[%d] DisplayName/Major = %q/%d", i, r.DisplayName(), r.Major())
		}
		if r.Published.Month() != w.publishedMonth || r.Published.Location() != time.UTC {
			t.Errorf("[%d] Published = %v", i, r.Published)
		}
		if !strings.HasPrefix(r.Link, "https://developer.apple.com/news/releases/?id=") {
			t.Errorf("[%d] Link = %q", i, r.Link)
		}
	}
}

func TestParseFeedSkipsForeignLinks(t *testing.T) {
	t.Parallel()
	bad := strings.Replace(item("iOS 27.0.2 (24A450)", "x", "Mon, 05 Oct 2026 10:00:00 PDT", ""),
		"<link>https://developer.apple.com/", "<link>https://developer.apple.com.evil.example/", 1)
	plainHTTP := strings.Replace(item("macOS 27.0.2 (26A440)", "y", "Mon, 05 Oct 2026 10:00:00 PDT", ""),
		"<link>https://", "<link>http://", 1)
	releases, warnings, err := ParseFeed(feedWith(bad, plainHTTP, liveItems[4]))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(releases) != 1 || releases[0].Version != "27.0.1" {
		t.Fatalf("releases = %+v, want only iOS 27.0.1", releases)
	}
	if len(warnings) != 2 {
		t.Errorf("warnings = %v, want 2", warnings)
	}
}

func TestParseFeedMalformed(t *testing.T) {
	t.Parallel()
	if _, _, err := ParseFeed([]byte("not a feed")); err == nil {
		t.Fatal("want error for malformed feed")
	}
}

func TestTitleRe(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]bool{
		"iOS 27.0.1 (24A446)":                true,
		"iOS 27 (24A300)":                    true,
		"macOS Tahoe 26.1 (25B78)":           true,
		"iOS 27.2 RC (24B115)":               false,
		"iOS 27.2 RC 2 (24B120)":             false,
		"macOS 27.2 beta 3 (26B5062e)":       false,
		"macOS Tahoe 26.1 beta (25B5042k)":   false,
		"iOS 27.1 Release Candidate (24B1)":  false,
		"iPadOS 27.0.1 (24A446)":             false,
		"watchOS 27.0.1 (24R365)":            false,
		"Xcode 27.1 (27A9275)":               false,
		"iOS 27.0.1":                         false,
		"App Store Connect API 4.5.1":        false,
		"iOS 27.0.1 (24A446) extra":          false,
		"macOS Tahoe Beta 26.1 (25B5042k) ":  false,
		"iOS 26.3.1 (a) (23D8133)":           false,
		"macOS 15.7.5 (24G517)":              true,
		"iOS 27.0.1 (24A446)\n<script>":      false,
		"iOS 27.0.1.1.1 (24A446)":            false,
		"ios 27.0.1 (24A446)":                false,
		"macOS Sequoia 15.7.5 (24G517)":      true,
		"macOS Tahoe 26.6.2 (25G83)":         true,
		"macOS 27.0.1 RC 2 (26A434)":         false,
		"iOS 18.7.10 (22H374)":               true,
		"tvOS 27.2 beta 3 (24K5103f)":        false,
		"visionOS 27.0.1 (24M372)":           false,
		"iOS 27.0.1 (24A446) (Updated)":      false,
		"macOS Server 27.0 (26A1)":           true, // a capitalised word is accepted as a name
		"macOS 27.0.1 Update (26A434)":       false,
		"iOS 27.0.1 Developer beta (24A446)": false,
	} {
		if got := titleRe.MatchString(title); got != want {
			t.Errorf("titleRe.MatchString(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"26.10", "26.9", 1},
		{"27", "27.0", 0},
		{"18.7.10", "18.7.9", 1},
		{"26.6.2", "27.0.1", -1},
	} {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestClientFetch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases.rss" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(feedWith(liveItems...))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(testHTTP()).SetFeedURL(srv.URL + "/releases.rss")
	if c.FeedURL() != srv.URL+"/releases.rss" {
		t.Errorf("FeedURL = %q", c.FeedURL())
	}
	releases, _, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(releases) != 5 {
		t.Errorf("releases = %d, want 5", len(releases))
	}

	if NewClient(nil).SetFeedURL("  ").FeedURL() != DefaultFeedURL {
		t.Error("blank SetFeedURL must keep the default")
	}

	_, _, err = NewClient(testHTTP()).SetFeedURL(srv.URL + "/missing").Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "http 404") {
		t.Errorf("err = %v, want http 404", err)
	}
}

func TestClientFetchTooLarge(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxFeedBody+1))
	}))
	t.Cleanup(srv.Close)
	_, _, err := NewClient(testHTTP()).SetFeedURL(srv.URL).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v, want size error", err)
	}
}

func TestTitleReBoundsComponents(t *testing.T) {
	t.Parallel()
	for _, title := range []string{
		"iOS " + strings.Repeat("9", 5000) + " (24A446)",
		"iOS 27.0.1 (" + strings.Repeat("A", 17) + ")",
		"macOS " + strings.Repeat("Tahoe", 10) + " 26.1 (25B78)",
	} {
		if titleRe.MatchString(title) {
			t.Errorf("oversized title %.40q… must not match", title)
		}
	}
}

// TestReleaseNotesURLStaysOnApple pins the link checks against the escapes
// a hostile feed could try.
func TestReleaseNotesURLStaysOnApple(t *testing.T) {
	t.Parallel()
	for href, want := range map[string]string{
		"//evil.example/x":                          "",
		"https:evil.example":                        "",
		"https://developer.apple.com@evil.example/": "",
		"https://developer.apple.com.evil.example/": "",
		"javascript:alert(1)":                       "",
		"http://developer.apple.com/x":              "",
		"/go/?id=x\u202e":                           "", // raw query keeps the bidi char → rejected
		`/\evil.example/x`:                          "https://developer.apple.com/%5Cevil.example/x",
		"/go/?id=ios-27_1-rn":                       "https://developer.apple.com/go/?id=ios-27_1-rn",
	} {
		frag := `<a href="` + href + `">View release notes</a>`
		if got := releaseNotesURL(frag); got != want {
			t.Errorf("releaseNotesURL(%q) = %q, want %q", href, got, want)
		}
	}
	if got := releaseNotesURL(`<a href="/go/?id=x">View downloads</a>`); got != "" {
		t.Errorf("non-notes anchor picked: %q", got)
	}
}

func TestValidAppleLinkRejectsNonASCII(t *testing.T) {
	t.Parallel()
	for _, l := range []string{
		"https://developer.apple.com/x\u202e",
		"https://developer.apple.com/x\u200b",
		"https://developer.apple.com/x\u00a0",
		"https://developer.apple.com/x y",
		"https://developer.apple.com/x\ty",
	} {
		if validAppleLink(l) {
			t.Errorf("validAppleLink(%q) = true, want false", l)
		}
	}
	if !validAppleLink("https://developer.apple.com/news/releases/?id=10192026a") {
		t.Error("real Apple link rejected")
	}
}

func TestParseFeedSkipsPrereleaseNameWords(t *testing.T) {
	t.Parallel()
	var items []string
	for i, title := range []string{
		"iOS Beta 27.1 (24B5)", "macOS RC 27.2 (26B118)", "macOS Developer 27.2 (26B118)",
		"iOS Seed 27.2 (24B5)", "iOS Public 27.2 (24B5)", "macOS PREVIEW 27.2 (26B1)",
	} {
		items = append(items, item(title, "p"+string(rune('a'+i)), "Mon, 19 Oct 2026 10:00:00 PDT", ""))
	}
	items = append(items, liveItems[8]) // macOS Tahoe 26.6.2 must still parse
	releases, _, err := ParseFeed(feedWith(items...))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(releases) != 1 || releases[0].Name != "Tahoe" {
		t.Errorf("releases = %+v, want only macOS Tahoe 26.6.2", releases)
	}
}
