package dnsfollow

import (
	"testing"

	"configflow/internal/model"
)

// HR O18 T1b L4: a damaged (status-less) stored state locks binding edits
// like a run; a missing or finished state does not.
func TestReviewO18T1bDamagedStateLocksBinding(t *testing.T) {
	tests := []struct {
		name   string
		state  *model.DNSFollowState
		locked bool
	}{
		{"damaged", &model.DNSFollowState{}, true},
		{"publishing", &model.DNSFollowState{Status: "publishing"}, true},
		{"missing", nil, false},
		{"failed", &model.DNSFollowState{Status: "failed"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mutable(&agent{Agent: &model.Agent{ID: "t", DNSFollowState: tt.state}})
			if (err != nil) != tt.locked {
				t.Fatalf("mutable = %v, want locked %v", err, tt.locked)
			}
		})
	}
}
