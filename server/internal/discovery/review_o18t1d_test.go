package discovery

import (
	"testing"

	"configflow/internal/model"
)

// TestReviewO18T1dDamagedCustomConfigPorts (M3 ruling): ports are not
// checked against a damaged base as if it listened on nothing.
func TestReviewO18T1dDamagedCustomConfigPorts(t *testing.T) {
	if _, err := customPorts(model.DamagedText); err == nil || err.Error() != msgCustomConfigNotText {
		t.Fatalf("err = %v", err)
	}
}
