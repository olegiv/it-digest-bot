package gorelease

import (
	"strings"
	"testing"
)

func TestFormatReleaseContainsKeyParts(t *testing.T) {
	t.Parallel()

	out := FormatRelease(
		"go1.26.2",
		"go1.26.2 includes security fixes to the go command.",
		"https://go.dev/dl/#go1.26.2",
		"https://go.dev/doc/devel/release#go1.26.2",
	)

	for _, want := range []string{
		"🚀",
		"*Go*",
		"`go1.26.2`",
		"security fixes",
		"[Downloads](",
		"[Release history](",
		"\\#Go",
		"\\#Golang",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestFormatReleaseUsesFallbackSummary(t *testing.T) {
	t.Parallel()

	out := FormatRelease("go1.99.1", "", DownloadURL("go1.99.1"), ReleaseHistoryURL("go1.99.1"))
	if !strings.Contains(out, "Official stable") {
		t.Fatalf("fallback summary missing:\n%s", out)
	}
}

func TestGoReleaseURLs(t *testing.T) {
	t.Parallel()

	if got := DownloadURL("go1.26.2"); got != "https://go.dev/dl/#go1.26.2" {
		t.Fatalf("DownloadURL = %q", got)
	}
	if got := ReleaseHistoryURL("go1.26.2"); got != "https://go.dev/doc/devel/release#go1.26.2" {
		t.Fatalf("ReleaseHistoryURL = %q", got)
	}
}
