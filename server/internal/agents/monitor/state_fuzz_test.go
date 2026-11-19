package monitor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzDashboardStateRoundTrip(f *testing.F) {
	state := &DashboardState{Schema: 1, Generation: strings.Repeat("a", 32), StartedAtMS: 1000, Reason: "initial", Target: strings.Repeat("b", 64), InstanceID: "core", SourceRevision: strings.Repeat("c", 64), RuntimeRevision: strings.Repeat("d", 64), Baseline: observationsOf([3][]metricSeries{})}
	raw, err := json.Marshal(state)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{string(raw), `null`, `{"schema":1,"schema":2}`, `{"Schema":1}`, `[]`} {
		f.Add(seed)
	}
	state.Schema = 2
	state.Carried = observationsOf([3][]metricSeries{})
	state.Pending = &PendingPublication{DeploymentID: "dep-1", ExpectedRevision: strings.Repeat("e", 64), CollectedAtMS: 2000, Totals: state.Baseline}
	checkpoint, err := json.Marshal(state)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(checkpoint))
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > MaxStateBytes {
			return
		}
		state, err := DecodeState([]byte(raw))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		copied, err := DecodeState(encoded)
		if err != nil {
			t.Fatalf("validated state could not be read: %v", err)
		}
		stable, err := json.Marshal(copied)
		if err != nil || !bytes.Equal(encoded, stable) {
			t.Fatalf("state round trip changed: %v", err)
		}
		before := append([]byte(nil), encoded...)
		_, _, err = StateTotals(state, state.Baseline)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(state)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("totals mutated its boundary", err)
		}
	})
}

func FuzzTypedMetricsSnapshot(f *testing.F) {
	for _, seed := range []string{"", `server_request_total{plugin_tag="dns",protocol="udp"} 7`, "cache_hit_total{plugin_tag=\"cache\",kind=\"fresh\"} 1.5\n", `server_request_total{plugin_tag="s",protocol="udp"} NaN`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > MaxInputBytes+1 {
			return
		}
		observations, err := ReadMetrics(text)
		if err != nil {
			return
		}
		raw, err := json.Marshal(observations)
		if err != nil {
			t.Fatal(err)
		}
		copied, err := decodeObservations(raw)
		if err != nil {
			t.Fatalf("metrics produced invalid observations: %v", err)
		}
		if CountersRestarted(copied, copied) {
			t.Fatal("identical counters reported a restart")
		}
		adjusted, _, err := ApplyBaseline(copied, copied)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(Summarize(adjusted)); err != nil {
			t.Fatal(err)
		}
		stable, err := json.Marshal(copied)
		if err != nil || !bytes.Equal(raw, stable) {
			t.Fatal("baseline calculation mutated observations", err)
		}
	})
}
