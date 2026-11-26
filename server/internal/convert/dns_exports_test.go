package convert

import "testing"

func TestN88dNativeDNSExportsKeepScopedRuleLiterals(t *testing.T) {
	for _, test := range []struct {
		text  string
		valid bool
	}{
		{"2001:db8::1%test", true}, {"2001:db8::%test/64", true}, {"2001:db8::%test/0128", true},
		{"2001:db8::%test/129", false}, {"2001:db8::%/64", false}, {"2001:db8::%a%b/64", false},
		{"192.0.2.1/0.0.0.255", true}, {"192.0.2.1/024", true}, {"192.0.2.1%test", false},
	} {
		t.Run(test.text, func(t *testing.T) {
			if got := IsIPNetwork(test.text); got != test.valid {
				t.Fatalf("accepted=%t want %t", got, test.valid)
			}
		})
	}
}
