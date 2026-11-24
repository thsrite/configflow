package configgen

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"testing"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
)

func reviewN811AgentRecord(t *testing.T, id string, deployments int) model.Agent {
	t.Helper()
	history := make([]string, 0, deployments)
	payload := strings.Repeat("x", 1024)
	for i := range deployments {
		history = append(history, fmt.Sprintf(`"d%d":{"id":"d%d","status":"success","config":%q}`, i, i, payload))
	}
	raw := fmt.Sprintf(`{"id":%q,"name":"Synthetic Agent","profile_id":"default","service_type":"mihomo","domain_discovery_enabled":true,"deployments":{%s}}`,
		id, strings.Join(history, ","))
	var agent model.Agent
	if err := json.Unmarshal([]byte(raw), &agent); err != nil {
		t.Fatal(err)
	}
	return agent
}

func reviewN811OutdatedBytes(t *testing.T, ctx context.Context, r *repo.Repository) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 20 {
		reports, err := outdatedAgents(ctx, r, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(reports) != 1 || reports[0].Version != "v1.18.0" {
			t.Fatalf("outdatedAgents = %+v", reports)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 20
}

// TestReviewN811CoreAgentOfMatchesJSONDecode checks that projecting model.Agent
// without full-record marshaling preserves CoreAgent's exact service_type and
// discovery semantics (absent vs explicit empty string vs non-mihomo).
func TestReviewN811CoreAgentOfMatchesJSONDecode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"absent_service_type_defaults_mihomo", `{"id":"a1","name":"A1","profile_id":"default","domain_discovery_enabled":true}`, true},
		{"null_service_type_defaults_mihomo", `{"id":"a2","name":"A2","profile_id":"default","service_type":null,"domain_discovery_enabled":true}`, true},
		{"explicit_mihomo", `{"id":"a3","name":"A3","profile_id":"default","service_type":"mihomo","domain_discovery_enabled":true}`, true},
		{"explicit_oxidns_excluded", `{"id":"a5","name":"A5","profile_id":"default","service_type":"oxidns","domain_discovery_enabled":true}`, false},
		{"numeric_service_type_excluded", `{"id":"a6","name":"A6","profile_id":"default","service_type":123,"domain_discovery_enabled":true}`, false},
		{"array_service_type_r5_nil_defaults", `{"id":"a7","name":"A7","profile_id":"default","service_type":[],"domain_discovery_enabled":true}`, true},
		{"discovery_disabled", `{"id":"a8","name":"A8","profile_id":"default","service_type":"mihomo","domain_discovery_enabled":false}`, false},
	}
	statusOf := func(string) subs.CoreStatus { return subs.CoreStatus{MihomoVersion: "v1.18.0"} }
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var agent model.Agent
			if err := json.Unmarshal([]byte(tc.raw), &agent); err != nil {
				t.Fatal(err)
			}
			var decoded subs.CoreAgent
			if err := json.Unmarshal([]byte(tc.raw), &decoded); err != nil {
				t.Fatal(err)
			}
			projected := coreAgentOf(&agent)
			gotReports := subs.OldCoreAgents([]subs.CoreAgent{projected}, "default", "default", statusOf)
			wantReports := subs.OldCoreAgents([]subs.CoreAgent{decoded}, "default", "default", statusOf)
			if len(gotReports) != len(wantReports) || (len(gotReports) == 1) != tc.want {
				t.Fatalf("coreAgentOf(%s) reports=%+v, decoded=%+v, wantMatch=%v", tc.raw, gotReports, wantReports, tc.want)
			}
		})
	}
}

// TestReviewN811OutdatedAgentsSkipAgentHistory guards the Mihomo generation
// path: inspecting agents under repo.View must not marshal an Agent's entire
// deployment history just to read CoreAgent fields.
func TestReviewN811OutdatedAgentsSkipAgentHistory(t *testing.T) {
	setup := func(deployments int) (context.Context, *repo.Repository) {
		r, err := repo.Open(t.TempDir(), repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		// // Allocation measurements exclude the test-only view guard's encodings.
		r.SetViewGuard(nil)
		ctx := repo.WithScope(t.Context())
		if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
			system.Agents = []model.Agent{reviewN811AgentRecord(t, "agent-1", deployments)}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		store := trafficstore.NewNative(r.DataDir())
		if _, err := store.Add(ctx, "agent-1", &trafficstore.Report{
			Status:        "running",
			MihomoVersion: json.RawMessage(`"v1.18.0"`),
			IPOnlyConns:   big.NewInt(0),
			Dropped:       big.NewInt(0),
		}, nil); err != nil {
			t.Fatal(err)
		}
		// Warm up repository document cache and traffic status cache.
		if reports, err := outdatedAgents(ctx, r, "default"); err != nil || len(reports) != 1 {
			t.Fatalf("warmup outdatedAgents = %+v, %v", reports, err)
		}
		return ctx, r
	}
	smallCtx, smallRepo := setup(1)
	largeCtx, largeRepo := setup(2000)
	smallBytes := reviewN811OutdatedBytes(t, smallCtx, smallRepo)
	largeBytes := reviewN811OutdatedBytes(t, largeCtx, largeRepo)
	// The history is ~2 MB; marshaling it even once per call exceeds this bound.
	if largeBytes > smallBytes+64*1024 {
		t.Fatalf("outdatedAgents allocates %d bytes with a large history (%d with a small one)", largeBytes, smallBytes)
	}
}
