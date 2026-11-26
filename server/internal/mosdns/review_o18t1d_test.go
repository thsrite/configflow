package mosdns

import (
	"testing"

	"configflow/internal/model"
)

// TestReviewO18T1dDamagedCustomConfigRefused (M3 ruling).
func TestReviewO18T1dDamagedCustomConfigRefused(t *testing.T) {
	profile := &model.Profile{}
	profile.MosDNS.CustomConfig = model.DamagedText
	if _, err := document(&model.Config{Profile: profile}, ""); err == nil || err.Error() != msgCustomConfigNotText {
		t.Fatalf("err = %v", err)
	}
}
