package routers

import (
	"testing"

	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/spec"
)

// TestR11ReviewFingerprintExcludesPassword: the readiness fingerprint reaches
// the browser, so it must not be an offline oracle for the device password,
// while a real router setting change still changes it.
func TestR11ReviewFingerprintExcludesPassword(t *testing.T) {
	d, _ := r11Driver(t)
	s := r11StepFor("plan")
	fingerprint := func(r *spec.Router) string {
		t.Helper()
		got, e := ipv6.InspectIPv6Readiness(t.Context(), d.Client, r, s.Agent, s.Host, s.Configuration, "", ipv6.PolicyContext{})
		if e != nil {
			t.Fatal(e)
		}
		return got.Fingerprint
	}
	first, second, renamed := r10Router(), r10Router(), r10Router()
	first.Password, second.Password, renamed.Password = "synthetic-secret-1", "synthetic-secret-2", "synthetic-secret-1"
	renamed.Endpoint = "https://192.0.2.9"
	base := fingerprint(first)
	if base != fingerprint(second) {
		t.Fatal("fingerprint depends on the device password")
	}
	if base == fingerprint(renamed) {
		t.Fatal("fingerprint ignores router settings")
	}
	if first.Password != "synthetic-secret-1" {
		t.Fatal("caller's router mutated")
	}
}
