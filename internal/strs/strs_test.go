package strs

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"no args", nil, ""},
		{"first wins", []string{"a", "b", "c"}, "a"},
		{"skips empty prefix", []string{"", "", "x", "y"}, "x"},
		{"single empty", []string{""}, ""},
		{"single value", []string{"only"}, "only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstNonEmpty(tc.in...); got != tc.want {
				t.Errorf("FirstNonEmpty(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
