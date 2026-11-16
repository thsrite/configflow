package oxidns

import (
	"errors"
	"testing"

	"configflow/internal/jsonx"
)

// The documented refusal: YAML values jsonx cannot hold that Python would
// copy into the generated configuration give *UnsupportedYAMLError, and
// Python's own errors still come first.
func TestUnsupportedYAMLRefusal(t *testing.T) {
	profile := func(settings *jsonx.Object) *jsonx.Object {
		return jsonx.Obj("rule_configs", []any{}, "system_config", jsonx.Obj("rule_proxy_token", "t"), "oxidns", settings)
	}
	for _, text := range []string{"1: x", "a: {1: x}", "a: !!set {x}", "a: &x [*x]"} {
		_, err := GenerateOxidnsConfig(profile(jsonx.Obj("custom_config", text)), "")
		var u *UnsupportedYAMLError
		if !errors.As(err, &u) {
			t.Errorf("custom_config %q: want UnsupportedYAMLError, got %v", text, err)
		}
	}
	// A mapping with a non-string key inside an upstream definition reaches
	// the output: refused, but only after the settings errors Python raises.
	_, err := GenerateOxidnsConfig(profile(jsonx.Obj("local_dns", "- {1: x, addr: udp://192.0.2.1}")), "")
	var u *UnsupportedYAMLError
	if !errors.As(err, &u) {
		t.Errorf("upstream mapping: want UnsupportedYAMLError, got %v", err)
	}
	_, err = GenerateOxidnsConfig(profile(jsonx.Obj("local_dns", "- {1: x, addr: udp://192.0.2.1}", "worker_threads", 0)), "")
	if e, ok := AsError(err); !ok || e.Message != "worker_threads 必须为 1–256 的整数" {
		t.Errorf("settings error first: got %v", err)
	}
	_, err = GenerateOxidnsConfig(profile(jsonx.Obj("custom_config", "- 1: x")), "")
	if e, ok := AsError(err); !ok || e.Message != "OxiDNS 高级 YAML 顶层必须为对象" {
		t.Errorf("non-mapping custom_config: got %v", err)
	}
}

// Pending functions are reported, never answered.
func TestPendingNativeValidation(t *testing.T) {
	_, err := NormalizeSettings(jsonx.Obj("config_mode", "native", "native_config", "plugins: []"), NormalizeOptions{})
	if !IsPending(err) {
		t.Fatalf("want PendingError, got %v", err)
	}
	settings, err := NormalizeSettings(jsonx.Obj("config_mode", "native", "native_config", "plugins: []"),
		NormalizeOptions{SkipNativeArguments: true})
	if err != nil || settings.GetString("config_mode") != "native" {
		t.Fatalf("draft normalization: %v %v", settings, err)
	}
}
