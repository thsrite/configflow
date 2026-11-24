package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
)

// TestAbortedReleaseRecordsProgress covers lab L5-D3: an aborted release
// records how many of the plan's operations were proven before shutdown.
func TestAbortedReleaseRecordsProgress(t *testing.T) {
	plan := &Plan{Fingerprint: "synthetic", Stages: []device.Stage{
		{Key: device.StageRoutes, Ops: []device.Op{RouteAddOp{}, RouteAddOp{}, RouteAddOp{}}},
		{Key: device.StageNATBypass, Ops: []device.Op{RouteAddOp{}}},
	}}
	tests := []struct {
		name   string
		result *Result
		want   string
	}{
		{"aborted", &Result{Aborted: true, Applied: 2, FailedStage: pointer(device.StageRoutes), Log: []ReleaseEntry{{device.StageRoutes, messageReleaseAborted, "failed"}}},
			messageReleaseAborted + fmt.Sprintf(messageReleaseAbortedProgress, 2, 4)},
		{"failed", &Result{Applied: 2, FailedStage: pointer(device.StageRoutes), Log: []ReleaseEntry{{device.StageRoutes, "synthetic failure", "failed"}}},
			"synthetic failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &spec.Router{RouterSettings: spec.RouterSettings{ID: "r1", Type: "ikuai"}}
			out, err := releasePublication(lifecycle.Detach(t.Context()), r, plan, tt.result, nil)
			if err != nil {
				t.Fatal(err)
			}
			var last LastRelease
			if err = json.Unmarshal(out.LastRelease.Value, &last); err != nil {
				t.Fatal(err)
			}
			if last.OK || last.Message != tt.want || last.FailedStage != device.StageRoutes || !strings.HasPrefix(last.At, "20") {
				t.Fatalf("last_release %+v, want message %q", last, tt.want)
			}
		})
	}
}
