package news

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

// CanonicalURLHash returns a sha256 hex digest of the canonicalized form
// of `rawURL`. Canonicalization: lowercased scheme+host, path preserved,
// utm_* and fbclid query params dropped, trailing slash removed.
//
// On parse failure the raw input is hashed verbatim so callers always
// get a stable identifier.
func CanonicalURLHash(rawURL string) string {
	return hashBytes(canonicalize(rawURL))
}

func canonicalize(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	if q := u.Query(); len(q) > 0 {
		for k := range q {
			if strings.HasPrefix(k, "utm_") || k == "fbclid" || k == "gclid" {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}

	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func hashBytes(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
