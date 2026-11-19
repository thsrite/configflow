package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestResolveAgentProfilesUsesOriginalBindings(t *testing.T) {
	e := newEnv(t)
	fallback, err := e.repo.DefaultProfileID(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw, want string
		reject    bool
	}{
		{`{}`, fallback, false}, {`{"profile_id":null}`, fallback, false}, {`{"profile_id":""}`, fallback, false},
		{`{"profile_id":"other"}`, "other", false}, {`{"profile_id":false}`, "", true}, {`{"profile_id":0}`, "", true}, {`{"profile_id":[]}`, "", true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var a model.Agent
			if err := json.Unmarshal([]byte(tc.raw), &a); err != nil {
				t.Fatal(err)
			}
			got, err := e.m.ResolveAgentProfiles(e.ctx, []model.Agent{a, a})
			if tc.reject {
				if err == nil {
					t.Fatal("projected value authorized binding")
				}
				return
			}
			if err != nil || len(got) != 2 || got[0] != tc.want || got[1] != tc.want {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}

func BenchmarkResolveAgentProfiles(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			m, ctx := newHeartbeatBenchmark(b, size.n)
			rows := make([]model.Agent, 100)
			for i := range rows {
				rows[i].ProfileID = "default"
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := m.ResolveAgentProfiles(ctx, rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
