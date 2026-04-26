// Package version exposes build-time metadata injected via -ldflags.
package version

// Values are overridden at build time via:
//
//	-ldflags "-X github.com/olegiv/it-digest-bot/internal/version.Version=... \
//	          -X github.com/olegiv/it-digest-bot/internal/version.Commit=... \
//	          -X github.com/olegiv/it-digest-bot/internal/version.Date=..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// UserAgent returns the HTTP User-Agent string used by every outbound request.
func UserAgent() string {
	return "it-digest-bot/" + Version + " (+https://github.com/olegiv/it-digest-bot)"
}

// String returns a compact one-line summary for `digest -version`.
func String() string {
	return "it-digest-bot " + Version + " (" + Commit + ", built " + Date + ")"
}
