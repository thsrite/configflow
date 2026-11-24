package rospolicy

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
)

// TestReadFactsFailsClosed pins the R8 StateReader contract: before R8 the
// policy write re-read the router through (*RouterOS).ReadState directly; a
// missing reader or an empty read must still block instead of panicking.
func TestReadFactsFailsClosed(t *testing.T) {
	readErr := errors.New("synthetic read failure")
	facts := &device.State{Routes: []*device.RouteEntry{}}
	nat := &ros.NatContext{}
	tests := []struct {
		name    string
		reader  StateReader
		wantErr string
		wantNAT bool
	}{
		{name: "nil reader", reader: nil, wantErr: messagePolicyUnreadable},
		{name: "nil facts", reader: func(context.Context) (*device.State, *ros.NatContext, error) { return nil, nat, nil }, wantErr: messagePolicyUnreadable},
		{name: "read error", reader: func(context.Context) (*device.State, *ros.NatContext, error) { return nil, nil, readErr }, wantErr: readErr.Error()},
		{name: "ok", reader: func(context.Context) (*device.State, *ros.NatContext, error) { return facts, nat, nil }, wantNAT: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFacts, gotNAT, err := rosDevice{readState: tt.reader}.readFacts(t.Context())
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr || gotFacts != nil || gotNAT != nil {
					t.Fatalf("readFacts = %v, %v, %v; want error %q", gotFacts, gotNAT, err, tt.wantErr)
				}
				return
			}
			if err != nil || gotFacts != facts || (gotNAT == nat) != tt.wantNAT {
				t.Fatalf("readFacts = %v, %v, %v", gotFacts, gotNAT, err)
			}
		})
	}
}
