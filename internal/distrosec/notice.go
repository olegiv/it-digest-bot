package distrosec

import (
	"cmp"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/pkgname"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Notice is one security advisory, normalized across distributions.
type Notice struct {
	ID         string    // "USN-9182-1", "DSA-6642-1"; the releases_seen version
	Distro     string    // "Ubuntu 24.04 LTS", "Debian"
	Title      string    // "nginx vulnerabilities", "nginx - security update"
	Packages   []string  // source packages the notice covers, in notice order
	Fixed      []string  // "nginx 1.24.0-2ubuntu7.21" per source package; Source narrows it to watched ones
	CVEs       []string  // valid CVE IDs, in notice order
	Paragraphs []string  // cleaned description paragraphs
	Published  time.Time // zero when missing or unparsable
	Link       string    // canonical advisory page, built from the validated ID
	ProOnly    bool      // Ubuntu: every binary of the fix is in an Ubuntu Pro pocket (ESM, FIPS, real-time)
	Regression bool      // a follow-up that fixes a regression of an earlier update
	Assessment

	// Raw advisory text, kept by the parsers so that the costly part
	// (HTML-to-text, paragraph cleaning, Assess) runs only for advisories
	// that touch a watched package; see Analyze.
	rawDesc    string
	rawHTML    bool // rawDesc is the DSA's HTML description
	rawContext []string
	analyzed   bool
	// binaries are the notice's binary package names (Ubuntu), used only
	// to tell an operator who listed a binary name to list its source.
	binaries []string
}

// Analyze fills Paragraphs and Assessment from the advisory text the parser
// kept. Sources call it only after the package filter, so a feed full of
// unrelated (or hostile, oversized) advisories costs a decode and the
// per-item ID, title and package checks, nothing more. It is idempotent, and a no-op for a Notice built without raw text.
func (n *Notice) Analyze() {
	if n.analyzed || (n.rawDesc == "" && len(n.rawContext) == 0) {
		return
	}
	n.analyzed = true
	desc := n.rawDesc
	if n.rawHTML {
		desc = htmlParagraphs(desc)
	}
	n.Paragraphs = paragraphs(desc)
	n.Assessment = Assess(desc, n.rawContext...)
}

var (
	cveRe = regexp.MustCompile(`^CVE-\d{4}-\d{4,7}$`)
	// versionRe bounds a package version shown in a post; Debian versions
	// use letters, digits and ".+~:-".
	versionRe = regexp.MustCompile(`^[A-Za-z0-9.+~:\-]{1,80}$`)
)

// ValidPackageName reports whether s is a valid Debian/Ubuntu source
// package name; see pkgname.ValidSourcePackage, which config uses too.
func ValidPackageName(s string) bool { return pkgname.ValidSourcePackage(s) }

// paragraphs splits advisory text on blank lines and cleans each paragraph
// (plainText), dropping empty ones.
func paragraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if p = plainText(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// plainText cleans untrusted advisory text (telegram.CleanText) and removes
// "scheme://…" and "www.…" tokens, which Telegram clients would auto-link
// in a plain-escaped post, except links to the distributions' own sites
// (allowedLink); bare domains are not removed. CleanText
// runs first, so invisible characters cannot hide a URL from the filter.
func plainText(s string) string {
	s = telegram.StripBareLinks(telegram.CleanText(s), allowedLink)
	return strings.Join(strings.Fields(s), " ")
}

// allowedLink keeps https links to ubuntu.com, debian.org and their
// subdomains (security-tracker.debian.org); anything else is removed.
func allowedLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, site := range []string{"ubuntu.com", "debian.org"} {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

// validCVEs keeps the well-formed CVE IDs, without duplicates.
func validCVEs(ids []string) []string {
	var out []string
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if cveRe.MatchString(id) && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// cleanLine flattens untrusted text to one clean line of at most n bytes.
func cleanLine(s string, n int) string {
	s = telegram.CleanText(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// sortOldestFirst orders notices by publication time, then ID, so a batch
// is posted in the order it was published. Notices without a date sort
// last, so a run of them cannot crowd dated ones out of the per-run cap.
func sortOldestFirst(ns []Notice) {
	slices.SortStableFunc(ns, func(a, b Notice) int {
		switch az, bz := a.Published.IsZero(), b.Published.IsZero(); {
		case az && !bz:
			return 1
		case bz && !az:
			return -1
		}
		if c := a.Published.Compare(b.Published); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}
