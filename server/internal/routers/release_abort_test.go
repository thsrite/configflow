package routers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
)

// TestAbortedReleaseRecordsProgress covers lab L5-D3: an aborted release
// records how many of the plan's operations were proven before shutdown.
func TestAbortedReleaseRecordsProgress(t *testing.T) {
	plan := &Plan{Fingerprint: "synthetic", Stages: []Stage{
		{Key: StageRoutes, Ops: []Op{RouteAddOp{}, RouteAddOp{}, RouteAddOp{}}},
		{Key: StageNATBypass, Ops: []Op{RouteAddOp{}}},
	}}
	tests := []struct {
		name   string
		result *Result
		want   string
	}{
		{"aborted", &Result{Aborted: true, Applied: 2, FailedStage: pointer(StageRoutes), Log: []ReleaseEntry{{StageRoutes, messageReleaseAborted, "failed"}}},
			messageReleaseAborted + fmt.Sprintf(messageReleaseAbortedProgress, 2, 4)},
		{"failed", &Result{Applied: 2, FailedStage: pointer(StageRoutes), Log: []ReleaseEntry{{StageRoutes, "synthetic failure", "failed"}}},
			"synthetic failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Router{RouterSettings: RouterSettings{ID: "r1", Type: "ikuai"}}
			out, err := releasePublication(lifecycle.Detach(t.Context()), r, plan, tt.result, nil)
			if err != nil {
				t.Fatal(err)
			}
			var last LastRelease
			if err = json.Unmarshal(out.LastRelease.Value, &last); err != nil {
				t.Fatal(err)
			}
			if last.OK || last.Message != tt.want || last.FailedStage != StageRoutes || !strings.HasPrefix(last.At, "20") {
				t.Fatalf("last_release %+v, want message %q", last, tt.want)
			}
		})
	}
}
