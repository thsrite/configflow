package convert

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
)

// TestReviewO18T1dDamagedCustomConfigRefused (M3 ruling): generation refuses
// a stored custom_config that is not text instead of using an empty base.
func TestReviewO18T1dDamagedCustomConfigRefused(t *testing.T) {
	var custom *CustomConfigYAMLError
	if _, err := customMihomoBase(model.DamagedText); !errors.As(err, &custom) || err.Error() != "mihomo.custom_config 不是文本，请在配置页重新填写后保存" {
		t.Fatalf("mihomo: %v", err)
	}
	if _, err := customMihomoBase(""); err != nil {
		t.Fatalf("empty base: %v", err)
	}
	profile := &model.Profile{}
	profile.Surge.CustomConfig = model.DamagedText
	if _, err := SurgeConfig(context.Background(), &model.Config{Profile: profile}, ""); !errors.As(err, &custom) || err.Error() != "surge.custom_config 不是文本，请在配置页重新填写后保存" {
		t.Fatalf("surge: %v", err)
	}
	profile = &model.Profile{}
	profile.Loon.CustomConfig = model.DamagedText
	if _, err := LoonConfig(context.Background(), &model.Config{Profile: profile}, ""); !errors.As(err, &custom) || err.Error() != "loon.custom_config 不是文本，请在配置页重新填写后保存" {
		t.Fatalf("loon: %v", err)
	}
}
