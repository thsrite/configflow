package discovery

// test_domain_discovery.py::test_registrable_domain_uses_public_suffix_list
// (ported early by A05); more inputs are checked against Python through
// traffic_history._target_key in internal/agents/metrics
// (TestTrafficReferenceTargetKeys). ProviderRevisions is covered by
// internal/api/agents.TestTrafficReportReturnsProviderRevisions and the
// Flask differential.

import "testing"

func TestRegistrableDomainUsesPublicSuffixList(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"a.b.example.co.uk": "example.co.uk",
		"www.google.com":    "google.com",
		"localhost":         "localhost",
		"com":               "com",
		"a.github.io":       "github.io", // private suffixes are not used
	} {
		if got := RegistrableDomain(input); got != want {
			t.Errorf("registrable_domain(%q) = %q, want %q", input, got, want)
		}
	}
}
