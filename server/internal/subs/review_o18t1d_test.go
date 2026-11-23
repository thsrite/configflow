package subs

import (
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// TestReviewO18T1dDNSSwitchNullIsAbsent: dns.enable/use-hosts that are null
// or not booleans read as absent (Python compares `is True` / `is False`).
func TestReviewO18T1dDNSSwitchNullIsAbsent(t *testing.T) {
	for _, src := range []string{"use-hosts: null\nenable: null\n", "use-hosts: ~\nenable: 'true'\n", "use-hosts: 0\nenable: 1\n", "{}\n"} {
		node, err := yamlx.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		settings, err := readDNSSettings(yamlx.Resolve(node))
		if err != nil {
			t.Fatal(err)
		}
		if settings.UseHosts != nil || settings.Enable != nil {
			t.Errorf("%q: enable=%v use-hosts=%v, want absent", src, settings.Enable, settings.UseHosts)
		}
	}
	node, _ := yamlx.Parse([]byte("use-hosts: false\nenable: true\nlisten: 53\n"))
	settings, err := readDNSSettings(yamlx.Resolve(node))
	if err != nil || settings.UseHosts == nil || *settings.UseHosts || settings.Enable == nil || !*settings.Enable || settings.Listen != "53" {
		t.Fatalf("settings %+v, %v", settings, err)
	}
}

// TestReviewO18T1dDamagedCustomConfigNotRelinked (M3 ruling): listener
// references are never rewritten into a damaged custom_config.
func TestReviewO18T1dDamagedCustomConfigNotRelinked(t *testing.T) {
	profile := &model.Profile{}
	profile.Mihomo.CustomConfig = model.DamagedText
	next := "B"
	if _, err := RelinkListeners(profile, "A", &next); err == nil || err.Error() != msgCustomConfigNotText {
		t.Fatalf("err = %v", err)
	}
	if profile.Mihomo.CustomConfig != model.DamagedText {
		t.Fatal("damaged base rewritten")
	}
}
