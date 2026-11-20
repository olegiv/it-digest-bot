package distrosec

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Notice is one security advisory, normalized across distributions.
type Notice struct {
	ID         string    // "USN-9182-1", "DSA-6642-1"; the releases_seen version
	Distro     string    // "Ubuntu 24.04 LTS", "Debian"
	Title      string    // "nginx vulnerabilities", "nginx - security update"
	Packages   []string  // source packages the notice covers, in notice order
	Fixed      []string  // "nginx 1.24.0-2ubuntu7.21" for watched packages; empty when unknown
	CVEs       []string  // valid CVE IDs, in notice order
	Paragraphs []string  // cleaned description paragraphs
	Published  time.Time // zero when missing or unparsable
	Link       string    // canonical advisory page, built from the validated ID
	ProOnly    bool      // Ubuntu: the fix is published only to Ubuntu Pro (ESM/FIPS) pockets
	Regression bool      // a follow-up that fixes a regression of an earlier update
	Assessment
}

var (
	cveRe     = regexp.MustCompile(`^CVE-\d{4}-\d{4,7}$`)
	pkgNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.\-]{0,63}$`)
	// versionRe bounds a package version shown in a post; Debian versions
	// use letters, digits and ".+~:-".
	versionRe = regexp.MustCompile(`^[A-Za-z0-9.+~:\-]{1,80}$`)
)

// ValidPackageName reports whether s is a valid Debian/Ubuntu source
// package name (lowercase letters, digits, "+", "-", "."; starting with a
// letter or digit; at most 64 bytes).
func ValidPackageName(s string) bool { return pkgNameRe.MatchString(s) }

// paragraphs splits advisory text on blank lines and cleans each paragraph
// (telegram.CleanText), dropping empty ones.
func paragraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if p = telegram.CleanText(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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

// baseID strips the revision of an advisory ID: "DSA-6637-2" -> "DSA-6637",
// "USN-8751-2" -> "USN-8751". Follow-ups share the base of the advisory
// they amend.
func baseID(id string) string {
	if i := strings.LastIndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	return id
}
