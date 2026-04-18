// Package strs holds tiny string helpers shared across packages.
package strs

// FirstNonEmpty returns the first argument that is not the empty string,
// or "" if all arguments are empty.
func FirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
