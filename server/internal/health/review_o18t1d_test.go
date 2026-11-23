package health

import (
	"context"
	"encoding/json"
	"testing"
)

// TestReviewO18T1dDamagedSettingsNotContinued: a stored entry whose test
// settings have the wrong type never continues its history.
func TestReviewO18T1dDamagedSettingsNotContinued(t *testing.T) {
	settings := ProbeSettings{URL: "https://example.com/t", TimeoutMS: 5000, DNSServers: []string{"192.0.2.53"}, DNSBootstrap: []string{}, UnifiedDelay: true}
	good := `"method":"` + Method + `","latency":10,"history":[10],"url":"https://example.com/t","unified_delay":true,"dns_servers":["192.0.2.53"],"dns_bootstrap":[]`
	file := `{"good":{` + good + `}`
	cases := map[string]string{
		"url-num":      `"url":5`,
		"url-null":     `"url":null`,
		"url-missing":  ``,
		"delay-text":   `"unified_delay":"true"`,
		"delay-num":    `"unified_delay":1`,
		"delay-null":   `"unified_delay":null`,
		"servers-text": `"dns_servers":"192.0.2.53"`,
		"servers-bad":  `"dns_servers":[5]`,
		"boot-obj":     `"dns_bootstrap":{}`,
	}
	for name, member := range cases {
		var entry map[string]json.RawMessage
		_ = json.Unmarshal([]byte(`{`+good+`}`), &entry)
		var override map[string]json.RawMessage
		if member != "" {
			_ = json.Unmarshal([]byte(`{`+member+`}`), &override)
		} else {
			delete(entry, "url")
		}
		for k, v := range override {
			entry[k] = v
		}
		data, _ := json.Marshal(entry)
		file += `,"` + name + `":` + string(data)
	}
	file += `}`
	collabs := defaultCollaborators(nil)
	collabs.ReadLatencyFile = func(context.Context) (json.RawMessage, bool) { return json.RawMessage(file), true }
	restore := SetProbeEnv(&ProbeEnv{Collaborators: collabs})
	defer restore()
	results := LoadLatencyResults(context.Background())
	if results.Len() != len(cases)+1 {
		t.Fatalf("loaded %d entries", results.Len())
	}
	for name, entry := range results.All() {
		if got := entry.sameTestSettings(settings); got != (name == "good") {
			t.Errorf("%s: sameTestSettings = %v", name, got)
		}
	}
}
