// Package distrosec watches Ubuntu and Debian security advisories for the
// source packages an operator runs on internet-facing servers. Ubuntu
// Security Notices (USN) come from the ubuntu.com notices API, Debian
// Security Advisories (DSA) from the debian.org DSA feed. Every notice that
// touches a configured package is announced, local issues included; the
// advisory text only labels the post (remote cue, impact). It is fully
// deterministic, no LLM is involved.
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
	// Impacts are normalized impact labels in a fixed order, e.g.
	// "code execution", "denial of service".
	Impacts []string
}

var (
	// remoteRe deliberately avoids words such as "network" or "TLS": kernel
	// notices list affected subsystems ("Network drivers", "TLS protocol"),
	// which say nothing about how the flaws can be reached.
	remoteRe = regexp.MustCompile(`(?i)\bremote(ly)?\b|\bcrafted\b|\bunauthenticated\b|request smuggling|cross-site|\bXSS\b|server-side request forgery|\bSSRF\b`)
	// issueRe marks a list item that describes an issue, as opposed to the
	// subsystem lists of kernel notices (see dropListItems).
	issueRe = regexp.MustCompile(`(?i)\battacker|\busers?\b|\bcould\b|\bcan (take|use|cause|exploit)|\bmay (allow|lead|result|be)|\bresult(s|ing)? in\b|\blead(s|ing)? to\b|\bdiscovered (that|a|an|in)\b|\bincorrectly\b|\bdid not (properly|correctly)\b|\bvulnerabilit|\bflaws?\b`)
	// bulletRe marks a list item. Kernel notices list affected subsystems
	// ("  - Remote Processor subsystem;"); such items are ignored unless they
	// describe an issue themselves.
	bulletRe = regexp.MustCompile(`^\s*[-*•]\s+`)

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
// paragraphs; context (title, summary) restates them and may add remote
// cues and impacts. Ubuntu and Debian security teams use consistent
// wording ("A remote attacker could …"), which is the only exposure signal
// both feeds carry: neither lists CVSS vectors or priorities with the
// notice itself. The result labels a post; it never suppresses one.
func Assess(description string, context ...string) Assessment {
	var (
		a   Assessment
		all []string
	)
	for _, text := range append([]string{description}, context...) {
		for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
			// Feeds wrap lines mid-phrase ("remote code\nexecution").
			para = strings.Join(strings.Fields(dropListItems(para)), " ")
			if para == "" {
				continue
			}
			all = append(all, para)
			a.Remote = a.Remote || remoteRe.MatchString(para)
		}
	}
	joined := strings.Join(all, " ")
	for _, r := range impactRules {
		if r.re.MatchString(joined) {
			a.Impacts = append(a.Impacts, r.label)
		}
	}
	return a
}

// dropListItems removes list-item lines that describe no issue, such as the
// subsystem lists of kernel notices.
func dropListItems(para string) string {
	if !strings.Contains(para, "\n") && !bulletRe.MatchString(para) {
		return para
	}
	lines := strings.Split(para, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if bulletRe.MatchString(line) && !issueRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
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
