package agents_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	"configflow/internal/lifecycle"
)

// TestReviewN810TrafficRetentionShutdownAndDisconnect covers the fourth standing
// HR item for the native retention write owner with a real Runtime: after
// shutdown nothing is committed; after a request disconnect the write completes.
func TestReviewN810TrafficRetentionShutdownAndDisconnect(t *testing.T) {
	tests := []struct {
		name     string
		shutdown bool
		wantErr  error
		wantDays int64
	}{
		{name: "shutdown", shutdown: true, wantErr: context.Canceled, wantDays: metrics.DefaultRetention},
		{name: "disconnect", shutdown: false, wantErr: nil, wantDays: 180},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTrafficEnv(t, false)
			r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
			r.Start(e.s.Context())
			defer r.Shutdown(context.Background())
			ctx := r.BindRequest(e.s.Context())
			if tt.shutdown {
				r.Shutdown(context.Background())
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := metrics.SetTrafficRetention(ctx, e.manager, 180)
			if !errors.Is(err, tt.wantErr) && !(tt.wantErr == nil && err == nil) {
				t.Fatalf("SetTrafficRetention error = %v, want %v", err, tt.wantErr)
			}
			system, err := e.manager.Repository().System(e.s.Context())
			if err != nil {
				t.Fatal(err)
			}
			stored, err := metrics.TrafficRetention(system)
			if err != nil || stored != tt.wantDays {
				t.Fatalf("stored retention = %d (%v), want %d", stored, err, tt.wantDays)
			}
		})
	}
}
