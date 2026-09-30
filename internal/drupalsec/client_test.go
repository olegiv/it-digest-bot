package drupalsec

import (
	"context"
	"fmt"
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

// handcraftedItem exercises variants the live snapshot does not: a plain "/"
// score, several CVEs, HTML entities in prose.
const handcraftedItem = `<item>
<title>Example Module - Highly critical - Remote code execution - SA-CONTRIB-2026-901</title>
<link>https://www.drupal.org/sa-contrib-2026-901</link>
<description>&lt;div class=&quot;field field-name-field-project field-type-entityreference field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/project/example_module&quot;&gt;Example Module&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-criticality field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Security risk:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/security-team/risk-levels&quot;&gt;&lt;strong&gt;Highly critical&lt;/strong&gt; 22/25 AC:None/A:None/CI:All/II:All/E:Exploit/TD:All&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-type field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Vulnerability:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;Remote code execution&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-affected-versions field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Affected versions:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&amp;gt;=2.0.0 &amp;lt;2.1.3 || &amp;lt;1.9.9&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-cve field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;CVE IDs:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;CVE-2026-90001, CVE-2026-90002&lt;/div&gt;&lt;div class=&quot;field-item odd&quot;&gt;CVE-2026-90003&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-description field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Description:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;First paragraph with &amp;quot;quotes&amp;quot; &amp;amp; an it&amp;#039;s.&lt;/p&gt;
&lt;p&gt;Second paragraph.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-solution field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Solution:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;Install the latest version:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;Upgrade to &lt;a href=&quot;/project/example_module/releases/2.1.3&quot;&gt;Example Module 2.1.3&lt;/a&gt;.&lt;/li&gt;
&lt;li&gt;Or upgrade to &lt;a href=&quot;/project/example_module/releases/1.9.9&quot;&gt;Example Module 1.9.9&lt;/a&gt;.&lt;/li&gt;
&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;</description>
<pubDate>Thu, 01 Oct 2026 10:00:00 +0000</pubDate>
<dc:creator>Drupal Security Team</dc:creator>
<guid isPermaLink="false">9000001 at https://www.drupal.org</guid>
</item>`

const unstructuredItem = `<item>
<title>Another Module - Less critical - Information disclosure - SA-CONTRIB-2026-902</title>
<link>https://www.drupal.org/sa-contrib-2026-902</link>
<description>&lt;p&gt;No structured fields at all.&lt;/p&gt;</description>
<pubDate>Thu, 01 Oct 2026 11:00:00 +0000</pubDate>
<guid isPermaLink="false">9000002 at https://www.drupal.org</guid>
</item>`

const edgeItems = `<item>
<title>No GUID Module - Moderately critical - Access bypass - SA-CONTRIB-2026-801</title>
<link>https://www.drupal.org/sa-contrib-2026-801</link>
<description>&lt;p&gt;x&lt;/p&gt;</description>
<pubDate>Wed, 23 Sep 2026 17:24:33 +0000</pubDate>
</item>
<item>
<title>Bad Date Module - Moderately critical - Access bypass - SA-CONTRIB-2026-802</title>
<link>https://www.drupal.org/sa-contrib-2026-802</link>
<description></description>
<pubDate>not a date</pubDate>
<guid isPermaLink="false">8000002 at https://www.drupal.org</guid>
</item>
<item>
<title>Link Only</title>
<link>https://www.drupal.org/node/8000003</link>
<description>&lt;p&gt;y&lt;/p&gt;</description>
<pubDate>Wed, 23 Sep 2026 17:24:33 GMT</pubDate>
<guid isPermaLink="false">8000003 at https://www.drupal.org</guid>
</item>
<item>
<title>No link - Critical - Access bypass - SA-CONTRIB-2026-804</title>
<description>&lt;p&gt;z&lt;/p&gt;</description>
<pubDate>Wed, 23 Sep 2026 17:24:33 +0000</pubDate>
<guid isPermaLink="false">8000004 at https://www.drupal.org</guid>
</item>`

func feedWith(items ...string) []byte {
	return []byte(feedHead + strings.Join(items, "\n") + feedTail)
}

func parseOK(t *testing.T, data []byte) []Advisory {
	t.Helper()
	advisories, warnings, err := ParseFeed(data)
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	return advisories
}

func findByID(t *testing.T, advisories []Advisory, id string) *Advisory {
	t.Helper()
	for i := range advisories {
		if advisories[i].ID == id {
			return &advisories[i]
		}
	}
	t.Fatalf("advisory %s not found", id)
	return nil
}

func assertEq(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func TestParseFeedLiveOrderAndCount(t *testing.T) {
	t.Parallel()
	advisories := parseOK(t, []byte(liveFeedXML))
	if len(advisories) != 4 {
		t.Fatalf("parsed %d advisories, want 4", len(advisories))
	}
	// Feed order (newest first) is preserved by ParseFeed; Source sorts.
	assertEq(t, "first", advisories[0].ID, "SA-CONTRIB-2026-191")
	assertEq(t, "last", advisories[3].ID, "SA-CORE-2026-013")
}

func TestParseFeedLiveContrib(t *testing.T) {
	t.Parallel()
	a := findByID(t, parseOK(t, []byte(liveFeedXML)), "SA-CONTRIB-2026-191")
	assertEq(t, "GUID", a.GUID, "3623987 at https://www.drupal.org")
	assertEq(t, "Version", a.Version(), "SA-CONTRIB-2026-191")
	assertEq(t, "Kind", string(a.Kind), "contrib")
	assertEq(t, "Title", a.Title, "Diba carousel slider - Moderately critical - Cross Site Scripting (XSS) - SA-CONTRIB-2026-191")
	assertEq(t, "Link", a.Link, "https://www.drupal.org/sa-contrib-2026-191")
	assertEq(t, "ProjectName", a.ProjectName, "Diba carousel slider")
	assertEq(t, "ProjectMachineName", a.ProjectMachineName, "diba_carousel")
	assertEq(t, "RiskLabel", a.RiskLabel, "Moderately critical")
	assertEq(t, "RiskScore", a.RiskScore, "12/25") // live feed uses thin spaces + U+2215
	assertEq(t, "RiskVector", a.RiskVector, "AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Uncommon")
	assertEq(t, "Vulnerability", a.Vulnerability, "Cross Site Scripting (XSS)")
	assertEq(t, "AffectedVersions", a.AffectedVersions, "<3.0.2")
	assertEq(t, "CVEs", strings.Join(a.CVEs, ","), "CVE-2026-96382")
	assertEq(t, "Published", a.Published.Format(time.RFC3339), "2026-09-23T17:24:33Z")
	if !strings.HasPrefix(a.Description, "The Diba Carousel Slider adds a Bootstrap carousel slider block") {
		t.Errorf("Description = %q", a.Description)
	}
	if strings.ContainsAny(a.Description, "<>") || strings.Contains(a.Description, "&quot;") {
		t.Errorf("Description still contains markup: %q", a.Description)
	}
	if !strings.Contains(a.Description, `"Allow HTML description"`) {
		t.Errorf("entities not decoded: %q", a.Description)
	}
	if !strings.Contains(a.Solution, "• If you are using Diba carousel slider 3.0.x, upgrade to Diba carousel slider 3.0.2.") {
		t.Errorf("Solution = %q", a.Solution)
	}
}

func TestParseFeedLiveCritical(t *testing.T) {
	t.Parallel()
	a := findByID(t, parseOK(t, []byte(liveFeedXML)), "SA-CONTRIB-2026-184")
	assertEq(t, "RiskLabel", a.RiskLabel, "Critical")
	assertEq(t, "RiskScore", a.RiskScore, "16/25")
	assertEq(t, "ProjectName", a.ProjectName, "Tawk.to - Live chat application")
	assertEq(t, "ProjectMachineName", a.ProjectMachineName, "tawk_to")
}

func TestParseFeedLiveCore(t *testing.T) {
	t.Parallel()
	a := findByID(t, parseOK(t, []byte(liveFeedXML)), "SA-CORE-2026-013")
	assertEq(t, "Kind", string(a.Kind), "core")
	assertEq(t, "ProjectName", a.ProjectName, "Drupal core")
	assertEq(t, "ProjectMachineName", a.ProjectMachineName, "drupal")
	// Operators are entity-escaped inside the HTML; tags must be stripped
	// before entities are decoded or these would be eaten as markup.
	assertEq(t, "AffectedVersions", a.AffectedVersions, ">=10.5.0 <10.6.17 || >=11.0.0 <11.3.17 || >=11.4.0 <11.4.7")
	assertEq(t, "CVEs", strings.Join(a.CVEs, ","), "")
	assertEq(t, "Vulnerability", a.Vulnerability, "Third-party libraries")
	if !strings.Contains(a.Solution, "• If you use Drupal 11.4.x, update to Drupal 11.4.7.") {
		t.Errorf("Solution = %q", a.Solution)
	}
}

func TestParseFeedLivePSA(t *testing.T) {
	t.Parallel()
	a := findByID(t, parseOK(t, []byte(liveFeedXML)), "PSA-2026-09-21")
	assertEq(t, "Kind", string(a.Kind), "psa")
	assertEq(t, "ProjectName", a.ProjectName, "")
	assertEq(t, "RiskLabel", a.RiskLabel, "")
	assertEq(t, "AffectedVersions", a.AffectedVersions, "")
	if !strings.Contains(a.Description, "There will be a security release for a widely used contributed module") {
		t.Errorf("Description = %q", a.Description)
	}
	if strings.Contains(a.Description, "<!--break-->") {
		t.Error("HTML comment leaked into description")
	}
}

func TestParseFeedHandcrafted(t *testing.T) {
	t.Parallel()
	advisories := parseOK(t, feedWith(handcraftedItem, unstructuredItem))
	if len(advisories) != 2 {
		t.Fatalf("parsed %d, want 2", len(advisories))
	}
	a := advisories[0]
	assertEq(t, "RiskLabel", a.RiskLabel, "Highly critical")
	assertEq(t, "RiskScore", a.RiskScore, "22/25")
	assertEq(t, "RiskVector", a.RiskVector, "AC:None/A:None/CI:All/II:All/E:Exploit/TD:All")
	assertEq(t, "AffectedVersions", a.AffectedVersions, ">=2.0.0 <2.1.3 || <1.9.9")
	assertEq(t, "CVEs", strings.Join(a.CVEs, ","), "CVE-2026-90001,CVE-2026-90002,CVE-2026-90003")
	assertEq(t, "Description", a.Description, "First paragraph with \"quotes\" & an it's.\nSecond paragraph.")
	assertEq(t, "Solution", a.Solution, "Install the latest version:\n• Upgrade to Example Module 2.1.3.\n• Or upgrade to Example Module 1.9.9.")

	b := advisories[1]
	assertEq(t, "unstructured Description", b.Description, "No structured fields at all.")
	assertEq(t, "unstructured ProjectName", b.ProjectName, "")
}

func TestParseFeedEdgeCases(t *testing.T) {
	t.Parallel()
	advisories, warnings, err := ParseFeed(feedWith(edgeItems))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "missing guid") || !strings.Contains(warnings[1], "missing link") {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(advisories) != 2 {
		t.Fatalf("parsed %d, want 2", len(advisories))
	}
	badDate := advisories[0]
	assertEq(t, "bad date ID", badDate.ID, "SA-CONTRIB-2026-802")
	if !badDate.Published.IsZero() {
		t.Errorf("unparsable pubDate should give zero time, got %v", badDate.Published)
	}
	linkOnly := advisories[1]
	assertEq(t, "no-id ID", linkOnly.ID, "")
	assertEq(t, "no-id Kind", string(linkOnly.Kind), "other")
	assertEq(t, "no-id Version", linkOnly.Version(), "nid-8000003")
	assertEq(t, "RFC1123 GMT date", linkOnly.Published.Format(time.RFC3339), "2026-09-23T17:24:33Z")
}

const badLinkItems = `<item>
<title>JS Link - Critical - XSS - SA-CONTRIB-2026-905</title>
<link>javascript:alert(1)</link>
<description>&lt;p&gt;a&lt;/p&gt;</description>
<guid isPermaLink="false">8000005 at https://www.drupal.org</guid>
</item>
<item>
<title>Off Host - Critical - XSS - SA-CONTRIB-2026-906</title>
<link>https://evil.example/sa-contrib-2026-906</link>
<description>&lt;p&gt;b&lt;/p&gt;</description>
<guid isPermaLink="false">8000006 at https://www.drupal.org</guid>
</item>
<item>
<title>Plain HTTP - Critical - XSS - SA-CONTRIB-2026-907</title>
<link>http://www.drupal.org/sa-contrib-2026-907</link>
<description>&lt;p&gt;c&lt;/p&gt;</description>
<guid isPermaLink="false">8000007 at https://www.drupal.org</guid>
</item>
<item>
<title>Good Link - Critical - XSS - SA-CONTRIB-2026-908</title>
<link>https://www.drupal.org/sa-contrib-2026-908</link>
<description>&lt;p&gt;d&lt;/p&gt;</description>
<guid isPermaLink="false">8000008 at https://www.drupal.org</guid>
</item>`

func TestParseFeedSkipsUnsafeLinks(t *testing.T) {
	t.Parallel()
	advisories, warnings, err := ParseFeed(feedWith(badLinkItems))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want 3 unsafe-link skips", warnings)
	}
	for _, w := range warnings {
		if !strings.Contains(w, "is not an https drupal.org URL") {
			t.Errorf("unexpected warning %q", w)
		}
	}
	if len(advisories) != 1 || advisories[0].ID != "SA-CONTRIB-2026-908" {
		t.Fatalf("advisories = %+v, want only the drupal.org item", advisories)
	}
}

func TestValidAdvisoryLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		link string
		ok   bool
	}{
		{"https://www.drupal.org/sa-core-2026-013", true},
		{"https://drupal.org/psa-2026-09-21", true},
		{"https://WWW.Drupal.ORG/sa-contrib-2026-1", true},
		{"http://www.drupal.org/sa-core-2026-013", false},
		{"https://evil.example/sa-core-2026-013", false},
		{"https://drupal.org.evil.example/x", false},
		{"https://notdrupal.org/x", false},
		{"https://user:pw@www.drupal.org/x", false},
		{"javascript:alert(1)", false},
		{"tg://user?id=1", false},
		{"https://www.drupal.org/sa\tcore", false},
		{"https://www.drupal.org/sa core", false},
		{"https://www.drupal.org/sa\ncore", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := validAdvisoryLink(tt.link); got != tt.ok {
			t.Errorf("validAdvisoryLink(%q) = %v, want %v", tt.link, got, tt.ok)
		}
	}
}

func TestParseFeedRejectsBrokenXML(t *testing.T) {
	t.Parallel()
	if _, _, err := ParseFeed([]byte("<rss><channel><item><title>broken")); err == nil {
		t.Error("expected error for malformed XML")
	}
	if _, _, err := ParseFeed(nil); err == nil {
		t.Error("expected error for empty document")
	}
}

func TestParseAdvisoryIDAndKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		link, title, wantID string
		wantKind            Kind
	}{
		{"https://www.drupal.org/sa-contrib-2026-191", "x - SA-CONTRIB-2026-191", "SA-CONTRIB-2026-191", KindContrib},
		{"https://www.drupal.org/sa-core-2026-013", "x", "SA-CORE-2026-013", KindCore},
		{"https://www.drupal.org/psa-2026-09-21", "x", "PSA-2026-09-21", KindPSA},
		{"https://www.drupal.org/node/1", "Webform - Critical - RCE - SA-CONTRIB-2026-175", "SA-CONTRIB-2026-175", KindContrib},
		{"https://www.drupal.org/node/1", "see sa-core-2026-001 inside", "SA-CORE-2026-001", KindCore},
		{"https://www.drupal.org/node/1", "nothing here", "", KindOther},
		{"", "", "", KindOther},
	}
	for _, tt := range tests {
		id := ParseAdvisoryID(tt.link, tt.title)
		if id != tt.wantID || KindFromID(id) != tt.wantKind {
			t.Errorf("ParseAdvisoryID(%q, %q) = %q/%q, want %q/%q", tt.link, tt.title, id, KindFromID(id), tt.wantID, tt.wantKind)
		}
	}
}

func TestParseRisk(t *testing.T) {
	t.Parallel()
	label, score, vector := parseRisk("Moderately critical 13 ∕ 25 AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Default")
	if label != "Moderately critical" || score != "13/25" || vector != "AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Default" {
		t.Errorf("parseRisk live = %q %q %q", label, score, vector)
	}
	label, score, vector = parseRisk("Critical")
	if label != "Critical" || score != "" || vector != "" {
		t.Errorf("parseRisk unparsable = %q %q %q", label, score, vector)
	}
	if label, _, _ := parseRisk(""); label != "" {
		t.Errorf("parseRisk empty = %q", label)
	}
}

func TestHTMLToText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		bullets bool
		want    string
	}{
		{"strip then unescape", `<div>&lt;3.0.2 &amp;&amp; &gt;=1.0</div>`, false, "<3.0.2 && >=1.0"},
		{"raw less-than in prose is text", `<p>Versions <3.0.2 are affected</p>`, false, "Versions <3.0.2 are affected"},
		{"script and style bodies dropped", `<p>Safe</p><script>alert(1)</script><style>p{}</style><noscript>x</noscript><p>Also safe</p>`, false, "Safe\nAlso safe"},
		{"unbalanced closers tolerated", `</div></div><p>Text</p></div>`, false, "Text"},
		{"br breaks line", `line one<br>line two<br/>line three`, false, "line one\nline two\nline three"},
		{"multi-line attribute", "<a title=\"one\ntwo\"><strong>Critical</strong> 16 ∕ 25 AC:Basic</a>", false, "Critical 16 ∕ 25 AC:Basic"},
		{"paragraphs", "<p>One.</p>\n<p>Two &#039;q&#039;.</p><!--break--><p>Three.</p>", false, "One.\nTwo 'q'.\nThree."},
		{"bullets", "<p>Install:</p><ul>\n<li>Upgrade to <a href=\"/r\">X 1.2</a>.</li>\n<li>Or Y.</li>\n</ul>", true, "Install:\n• Upgrade to X 1.2.\n• Or Y."},
		{"no bullets flag", "<ul><li>A</li><li>B</li></ul>", false, "A\nB"},
		{"nbsp and thin space", "Project:&nbsp;\u2009\u2009Core   here", false, "Project: Core here"},
		{"empty", "  \n ", false, ""},
	}
	for _, tt := range tests {
		if got := htmlToText(tt.in, tt.bullets); got != tt.want {
			t.Errorf("%s: htmlToText(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestVersionFallbacks(t *testing.T) {
	t.Parallel()
	if v := (&Advisory{ID: "SA-CORE-2026-001", GUID: "1 at x"}).Version(); v != "SA-CORE-2026-001" {
		t.Errorf("Version with ID = %q", v)
	}
	if v := (&Advisory{GUID: " 8000003 at https://www.drupal.org"}).Version(); v != "nid-8000003" {
		t.Errorf("Version from guid = %q", v)
	}
	if v := (&Advisory{GUID: "opaque-guid"}).Version(); v != "opaque-guid" {
		t.Errorf("Version opaque = %q", v)
	}
}

func TestClientFetch(t *testing.T) {
	t.Parallel()
	var gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		fmt.Fprint(w, liveFeedXML)
	}))
	defer srv.Close()

	c := NewClient(testHTTP()).WithFeedURL(srv.URL + "/security/all/rss.xml")
	if c.FeedURL() != srv.URL+"/security/all/rss.xml" {
		t.Errorf("FeedURL = %q", c.FeedURL())
	}
	advisories, warnings, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(advisories) != 4 || len(warnings) != 0 {
		t.Errorf("Fetch returned %d advisories, %v warnings", len(advisories), warnings)
	}
	if !strings.Contains(gotAccept, "application/rss+xml") {
		t.Errorf("Accept = %q", gotAccept)
	}
	if !strings.Contains(gotUA, "it-digest-bot/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestClientFetchErrors(t *testing.T) {
	t.Parallel()

	t.Run("http error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		_, _, err := NewClient(testHTTP()).WithFeedURL(srv.URL).Fetch(context.Background())
		if err == nil || !strings.Contains(err.Error(), "http 404") {
			t.Errorf("expected http 404 error, got %v", err)
		}
	})

	t.Run("oversized body", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(make([]byte, maxFeedBody+1))
		}))
		defer srv.Close()
		_, _, err := NewClient(testHTTP()).WithFeedURL(srv.URL).Fetch(context.Background())
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("expected size error, got %v", err)
		}
	})

	t.Run("malformed feed", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<rss><channel><item><title>broken")
		}))
		defer srv.Close()
		_, _, err := NewClient(testHTTP()).WithFeedURL(srv.URL).Fetch(context.Background())
		if err == nil || !strings.Contains(err.Error(), "parse") {
			t.Errorf("expected parse error, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, liveFeedXML)
		}))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := NewClient(testHTTP()).WithFeedURL(srv.URL).Fetch(ctx); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()
	c := NewClient(nil)
	if c.FeedURL() != DefaultFeedURL {
		t.Errorf("default feed URL = %q", c.FeedURL())
	}
	if c.WithFeedURL("  ").FeedURL() != DefaultFeedURL {
		t.Error("blank override must keep the default")
	}
}
