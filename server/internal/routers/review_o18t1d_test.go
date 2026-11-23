package routers

import (
	"configflow/internal/routers/spec"
	"testing"

	"configflow/internal/model"
)

// TestReviewO18T1dDamagedCustomConfigRefused (M3 ruling): IPv6 deployment
// and the desired fake-ip range refuse a damaged base instead of reading it
// as empty (which would withdraw managed IPv6 settings / use the default).
func TestReviewO18T1dDamagedCustomConfigRefused(t *testing.T) {
	profile := &model.Profile{}
	profile.Mihomo.CustomConfig = model.DamagedText
	if _, err := ipv6Document(profile); err == nil || err.Error() != spec.MessageIPv6YAMLType {
		t.Fatalf("ipv6Document: %v", err)
	}
	if _, err := spec.FakeIPNetwork(model.DamagedText); err == nil || err.Error() != spec.MessageIPv6YAMLType {
		t.Fatalf("FakeIPNetwork: %v", err)
	}
	if _, err := ipv6Document(&model.Profile{}); err != nil {
		t.Fatalf("empty base: %v", err)
	}
}
