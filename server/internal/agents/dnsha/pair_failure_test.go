package dnsha

import (
	"errors"
	"fmt"
	"testing"
)

// TestPairFailureMessageHook covers lab L3-D3 on the owner side: the hook
// rulesync uses names HA refusals with their Chinese message and leaves
// other errors to the generic text.
func TestPairFailureMessageHook(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	tests := []struct {
		name   string
		cause  error
		want   string
		wantOK bool
	}{
		{"baseline", fmt.Errorf("publish: %w", refuse(msgDifferentBaseline)), msgDifferentBaseline, true},
		{"busy", refuse(msgPairBusy), msgPairBusy, true},
		{"internal", errors.New("synthetic internal failure"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := f.s.PairFailureMessage(f.ctx, tt.cause)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("hook = %q %v, want %q %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
