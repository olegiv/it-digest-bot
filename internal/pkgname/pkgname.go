// Package pkgname validates Debian/Ubuntu identifiers that appear both in
// the configuration and in advisory feeds, so internal/config and
// internal/distrosec apply exactly the same rules. It is a leaf package:
// config must not depend on the feed clients.
package pkgname

import "regexp"

var (
	sourcePackageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.\-]{0,63}$`)
	ubuntuReleaseRe = regexp.MustCompile(`^[a-z]{3,20}$`)
)

// ValidSourcePackage reports whether s is a valid Debian/Ubuntu source
// package name: lowercase letters, digits, "+", "-" and ".", starting with a
// letter or digit, at most 64 bytes ("nginx", "php8.4", "postgresql-17").
func ValidSourcePackage(s string) bool { return sourcePackageRe.MatchString(s) }

// ValidUbuntuRelease reports whether s looks like an Ubuntu release
// codename ("noble", "jammy").
func ValidUbuntuRelease(s string) bool { return ubuntuReleaseRe.MatchString(s) }
