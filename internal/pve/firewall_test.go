package pve

import "testing"

func TestCanonicalIPSetCIDR(t *testing.T) {
	tests := map[string]string{
		"192.0.2.10":       "192.0.2.10/32",
		"192.0.2.10/32":    "192.0.2.10/32",
		"192.0.2.10/24":    "192.0.2.0/24",
		"+192.0.2.10/32":   "192.0.2.10/32",
		"2001:db8::1":      "2001:db8::1/128",
		"2001:db8::1/64":   "2001:db8::/64",
		"not-a-cidr-alias": "not-a-cidr-alias",
	}
	for input, want := range tests {
		if got := canonicalIPSetCIDR(input); got != want {
			t.Fatalf("canonicalIPSetCIDR(%q) = %q, want %q", input, got, want)
		}
	}
}
