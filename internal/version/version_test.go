package version

import (
	"strings"
	"testing"
)

func TestUserAgentContainsVersion(t *testing.T) {
	t.Parallel()
	ua := UserAgent()
	if !strings.Contains(ua, "it-digest-bot/") {
		t.Errorf("UserAgent = %q, want prefix %q", ua, "it-digest-bot/")
	}
	if !strings.Contains(ua, Version) {
		t.Errorf("UserAgent = %q, want to contain version %q", ua, Version)
	}
}

func TestStringContainsAllFields(t *testing.T) {
	t.Parallel()
	s := String()
	for _, want := range []string{Version, Commit, Date} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, want to contain %q", s, want)
		}
	}
}
