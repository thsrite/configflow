package routes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// useZoneinfo replaces the system directories; zones are created as files.
func useZoneinfo(t *testing.T, zones ...string) string {
	t.Helper()
	dir := t.TempDir()
	previous := zoneinfoDirectories
	t.Cleanup(func() { zoneinfoDirectories = previous })
	zoneinfoDirectories = []string{filepath.Join(dir, "missing"), filepath.Join(dir, "zoneinfo")}
	if len(zones) == 0 {
		zoneinfoDirectories = zoneinfoDirectories[:1]
	}
	for _, zone := range zones {
		path := filepath.Join(dir, "zoneinfo", zone)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("TZif"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func cronArgs(zone string, jobs ...map[string]any) map[string]any {
	list := []any{}
	for _, job := range jobs {
		list = append(list, job)
	}
	args := map[string]any{"jobs": list}
	if zone != "" {
		args["timezone"] = zone
	}
	return args
}

func TestOxiDNSTimezoneNeedsDatabaseOnlyForNamedZones(t *testing.T) {
	scheduled := map[string]any{"name": "rules", "schedule": "30 3 * * *"}
	interval := map[string]any{"name": "rules", "interval": "1h"}
	for _, test := range []struct {
		kind string
		args map[string]any
		want string
	}{
		{"cron", cronArgs("Asia/Shanghai", scheduled), "Asia/Shanghai"},
		{"cron", cronArgs(" Asia/Shanghai ", interval, scheduled), "Asia/Shanghai"},
		{"cron", cronArgs("Asia/Shanghai", interval), ""},
		{"cron", cronArgs("", scheduled), ""},
		{"cron", cronArgs("utc", scheduled), ""},
		{"cron", cronArgs("+08:00", scheduled), ""},
		{"time", map[string]any{"timezone": "Europe/Berlin"}, "Europe/Berlin"},
		{"time", map[string]any{}, ""},
		{"sequence", map[string]any{"timezone": "Asia/Shanghai"}, ""},
	} {
		if got := oxidnsPluginTimezone(test.kind, test.args); got != test.want {
			t.Fatalf("%s %v: got %q, want %q", test.kind, test.args, got, test.want)
		}
	}
}

func TestZoneinfoLookupUsesFirstDatabaseCaseInsensitively(t *testing.T) {
	useZoneinfo(t, "Asia/Shanghai")
	for _, zone := range []string{"Asia/Shanghai", "asia/shanghai"} {
		if !zoneinfoAvailable(zone) {
			t.Fatalf("%s should resolve", zone)
		}
	}
	for _, zone := range []string{"Asia/Tokyo", "Asia", "Asia/../Asia/Shanghai", ""} {
		if zoneinfoAvailable(zone) {
			t.Fatalf("%s should not resolve", zone)
		}
	}
	useZoneinfo(t)
	if zoneinfoAvailable("Asia/Shanghai") {
		t.Fatal("no database means no zone")
	}
}

func TestOxiDNSMissingTimezoneDataStopsBeforeActivation(t *testing.T) {
	useZoneinfo(t)
	content := "plugins:\n  - tag: rules_cron\n    type: cron\n    args:\n      timezone: Asia/Shanghai\n      jobs:\n        - name: update\n          schedule: 30 3 * * *\n          executors: [\"$rules_update\"]\n"
	stage, manifest := diagnosticFixture(t, content)
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ServiceType: "oxidns", ConfigPath: filepath.Join(stage, "live.yaml")}
	err := validateOxiDNS(cfg, doc, stage, filepath.Join(stage, "config.yaml"), manifest)
	if err == nil || !strings.HasPrefix(err.Error(), "timezone data for Asia/Shanghai is missing") {
		t.Fatalf("unexpected error: %v", err)
	}
	requireDiagnostic(t, err, "来源：插件 rules_cron", "plugins[0].args.timezone", "内容：timezone: Asia/Shanghai")
}

func TestOxiDNSTimezoneCheckPassesWithTzdata(t *testing.T) {
	useZoneinfo(t, "Asia/Shanghai")
	args := cronArgs("Asia/Shanghai", map[string]any{"name": "rules", "schedule": "30 3 * * *"})
	if err := validateOxiDNSTimezone("cron", args); err != nil {
		t.Fatal(err)
	}
}
