package pkgname

import "testing"

func TestValidSourcePackage(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"nginx": true, "php8.4": true, "postgresql-17": true, "libapache2-mod-php8.4": true,
		"g++-14": true, "0ad": true, "xz": true, "a": false, "7": false,
		"": false, "NGINX": false, "-nginx": false, ".x": false, "php8.4; rm -rf /": false,
		"a b": false, "nginx\n": false, string(make([]byte, 65)): false,
	} {
		if got := ValidSourcePackage(s); got != want {
			t.Errorf("ValidSourcePackage(%q) = %v, want %v", s, got, want)
		}
	}
	long := "a"
	for len(long) < 64 {
		long += "b"
	}
	if !ValidSourcePackage(long) || ValidSourcePackage(long+"c") {
		t.Error("64 bytes is the limit")
	}
}

func TestValidUbuntuRelease(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"noble": true, "jammy": true, "resolute": true,
		"": false, "ab": false, "Noble": false, "noble numbat": false, "24.04": false, "noble\n": false,
	} {
		if got := ValidUbuntuRelease(s); got != want {
			t.Errorf("ValidUbuntuRelease(%q) = %v, want %v", s, got, want)
		}
	}
}
