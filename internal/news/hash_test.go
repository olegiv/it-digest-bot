package news

import "testing"

func TestCanonicalURLHashEqualsForEquivalentURLs(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"https://example.com/post", "HTTPS://Example.COM/post/"},
		{"https://example.com/post", "https://example.com/post#section"},
		{"https://example.com/post", "https://example.com/post?utm_source=x&utm_campaign=y"},
		{"https://example.com/", "https://example.com"},
	}
	for _, p := range pairs {
		if CanonicalURLHash(p[0]) != CanonicalURLHash(p[1]) {
			t.Errorf("expected equal hash for %q and %q", p[0], p[1])
		}
	}
}

func TestCanonicalURLHashDifferentForDifferentPaths(t *testing.T) {
	t.Parallel()
	if CanonicalURLHash("https://a.com/x") == CanonicalURLHash("https://a.com/y") {
		t.Error("different URLs hashed equal")
	}
}

func TestCanonicalURLHashHandlesGarbage(t *testing.T) {
	t.Parallel()
	// Should not panic, should return a stable hash.
	h := CanonicalURLHash("not a url")
	if len(h) != 64 {
		t.Errorf("hash len = %d", len(h))
	}
}
