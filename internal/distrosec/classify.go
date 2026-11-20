// Package distrosec watches Ubuntu and Debian security advisories for the
// source packages an operator runs on internet-facing servers. Ubuntu
// Security Notices (USN) come from the ubuntu.com notices API, Debian
// Security Advisories (DSA) from the debian.org DSA feed. A notice is
// announced when it touches a configured package and its description does
// not limit the issue to local attackers; it is fully deterministic, no LLM
// is involved.
package distrosec

import (
	"regexp"
	"slices"
	"strings"
)

// Assessment is what the advisory text says about exposure and impact.
type Assessment struct {
	// Remote is set when the text names a remote attacker, crafted input or
	// a web-facing impact (request smuggling, XSS, SSRF, …).
	Remote bool
	// LocalOnly is set when the text names only local attackers or users
	// and nothing remote. Such notices are not announced: they cannot be
	// used to break into a server from outside.
	LocalOnly bool
	// Impacts are normalized impact labels in a fixed order, e.g.
	// "code execution", "denial of service".
	Impacts []string
}

var (
	// remoteRe deliberately avoids words such as "network" or "TLS": kernel
	// notices list affected subsystems ("Network drivers", "TLS protocol"),
	// which say nothing about how the flaws can be reached.
	remoteRe = regexp.MustCompile(`(?i)\bremote(ly)?\b|\bcrafted\b|\bunauthenticated\b|request smuggling|cross-site|server-side request forgery|\bSSRF\b`)
	// issueRe marks a paragraph that describes an exploitable issue, as
	// opposed to notes ("This update introduces the nullok option").
	issueRe = regexp.MustCompile(`(?i)\battacker|\busers?\b|\bcould\b|\bcan (take|use|cause|exploit)|\bmay (allow|lead|result|be)|\bresult(s|ing)? in\b|\blead(s|ing)? to\b`)
	localRe = regexp.MustCompile(`(?i)\blocal (attacker|user)s?\b|\blocally\b|\blocal privilege escalation\b`)

	impactRules = []struct {
		label string
		re    *regexp.Regexp
	}{
		{"code execution", regexp.MustCompile(`(?i)(execut\w* (of )?arbitrary (code|commands)|remote code execution|run (arbitrary )?programs|code execution)`)},
		{"privilege escalation", regexp.MustCompile(`(?i)privilege escalation|escalate privileges|gain (root|elevated|administrative) privileges`)},
		{"authentication bypass", regexp.MustCompile(`(?i)(authentication|verification|access|security|signature|revocation)[\w ]{0,30}bypass|bypass(ing)? [\w ]{0,30}(authentication|verification|restrictions|checks|protections)|incorrect authentication|authenticated .{0,40}contrary`)},
		{"information disclosure", regexp.MustCompile(`(?i)information (disclosure|leaks?)|(obtain|expose|leak)\w* (sensitive )?information|sensitive information`)},
		{"cross-site scripting", regexp.MustCompile(`(?i)cross-site scripting|\bXSS\b`)},
		{"HTTP request smuggling", regexp.MustCompile(`(?i)request smuggling`)},
		{"spoofing", regexp.MustCompile(`(?i)spoof`)},
		{"SQL injection", regexp.MustCompile(`(?i)SQL injection`)},
		{"denial of service", regexp.MustCompile(`(?i)denial of service|\bcrash\b|resource consumption`)},
	}
)

// Assess classifies an advisory: description holds blank-line separated
// paragraphs, one per issue; context (title, summary) only restates them and
// adds remote cues and impacts, never issues of its own. Ubuntu and
// Debian security teams use consistent wording ("A remote attacker could …",
// "A local user can …"), which is the only exposure signal both feeds
// carry: neither lists CVSS vectors or priorities with the notice itself.
//
// Locality is judged per paragraph, because one notice often bundles
// several issues: it is LocalOnly only when every paragraph that describes
// an issue names a local attacker or user and nothing remote. A paragraph
// with an unspecified attacker ("An attacker could …") or one on the local
// network keeps the notice relevant.
func Assess(description string, context ...string) Assessment {
	var (
		a                  Assessment
		issues, localIssue int
		all                []string
	)
	texts := append([]string{description}, context...)
	for i, text := range texts {
		for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
			// Feeds wrap lines mid-phrase ("remote code\nexecution").
			para = strings.Join(strings.Fields(para), " ")
			if para == "" {
				continue
			}
			all = append(all, para)
			remote := remoteRe.MatchString(para)
			a.Remote = a.Remote || remote
			if i == 0 && issueRe.MatchString(para) {
				issues++
				if !remote && localRe.MatchString(para) {
					localIssue++
				}
			}
		}
	}
	joined := strings.Join(all, " ")
	if issues > 0 {
		a.LocalOnly = !a.Remote && localIssue == issues
	} else {
		a.LocalOnly = !a.Remote && localRe.MatchString(joined)
	}
	for _, r := range impactRules {
		if r.re.MatchString(joined) {
			a.Impacts = append(a.Impacts, r.label)
		}
	}
	return a
}

// Relevant reports whether a notice should be announced: it touches one of
// the watched packages and is not limited to local attackers.
func Relevant(a Assessment, matched []string) bool {
	return len(matched) > 0 && !a.LocalOnly
}

// matchPackages returns the watched packages among pkgs, in pkgs order.
func matchPackages(pkgs []string, watched map[string]bool) []string {
	var out []string
	for _, p := range pkgs {
		if watched[p] && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// watchSet builds the lookup set for a configured package list.
func watchSet(pkgs []string) map[string]bool {
	m := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}
