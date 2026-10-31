package devtunnel

import "testing"

func TestNormalizeExpiration(t *testing.T) {
	cases := []struct {
		in, out string
		ok      bool
	}{
		{"", "30d", true},
		{" 7D ", "7d", true},
		{"12h", "12h", true},
		{"30d", "30d", true},
		{"720h", "720h", true},
		{"07d", "7d", true},
		{"0d", "", false},
		{"31d", "", false},
		{"721h", "", false},
		{"7w", "", false},
		{"+7d", "", false},
	}
	for _, tc := range cases {
		got, err := NormalizeExpiration(tc.in)
		if tc.ok && (err != nil || got != tc.out) {
			t.Fatalf("NormalizeExpiration(%q) = %q, %v", tc.in, got, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("NormalizeExpiration(%q) accepted %q", tc.in, got)
		}
	}
}
