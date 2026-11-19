package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestTypedHASummaryOwnsOnlyPublicMembers(t *testing.T) {
	job := &model.HAJournal{ID: "pair", Status: "ready", Message: "ready", Members: []model.HAMember{{AgentID: "member", Name: "original"}}, Enable: &model.HAEnable{}}
	if err := job.Enable.Fields.Set("captured_source", json.RawMessage(`{"private":"synthetic-hidden"}`)); err != nil {
		t.Fatal(err)
	}
	summary, err := HASummaryModel(job)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := model.Marshal(summary)
	if err != nil || strings.Contains(string(raw), "synthetic-hidden") || strings.Contains(string(raw), "captured_source") {
		t.Fatal("private graph exposed", string(raw), err)
	}
	summary.Members[0].Name = "public-copy"
	if job.Members[0].Name != "original" {
		t.Fatal("summary aliases source")
	}
	task := &model.Deployment{DeploymentID: "task", Status: "unknown", Pending: new(true), HTTPStatus: new(model.Int(502))}
	aggregate, err := HAAggregateModel(task, summary)
	if err != nil {
		t.Fatal(err)
	}
	aggregate.HAPublication.Members[0].Name = "aggregate-copy"
	if summary.Members[0].Name != "public-copy" || task.Status != "unknown" || task.Pending == nil || aggregate.Pending != nil || aggregate.HTTPStatus != nil || aggregate.Status != "ready" {
		t.Fatal("aggregate ownership or fields changed")
	}
}

func TestTypedHASummaryAllocationIgnoresCapturedSource(t *testing.T) {
	measure := func(size int) testing.BenchmarkResult {
		return testing.Benchmark(func(b *testing.B) {
			job := &model.HAJournal{ID: "pair", Status: "ready", Message: "ready", Enable: &model.HAEnable{}}
			raw := json.RawMessage(`"` + strings.Repeat("x", size) + `"`)
			if err := job.Enable.Fields.Set("captured_source", raw); err != nil {
				b.Fatal(err)
			}
			if err := job.Fields.Set("private_source", raw); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := HASummaryModel(job)
				if err != nil || result.ID != "pair" {
					b.Fatal(result, err)
				}
			}
		})
	}
	small, large := measure(0), measure(2<<20)
	if small.N == 0 || large.N == 0 {
		t.Fatal("benchmark failed")
	}
	t.Logf("HA summary empty: %d B/op %d allocs/op; 2MiB captured sources: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+32<<10 || large.AllocsPerOp() > small.AllocsPerOp()+100 {
		t.Fatal("HA summary encoded private captured configuration")
	}
}
