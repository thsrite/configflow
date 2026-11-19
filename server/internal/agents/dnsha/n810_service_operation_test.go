package dnsha

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
)

func TestN810NativeServiceOperationRespectsBusyMemberLease(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	agents.ResetHooks()
	t.Cleanup(agents.ResetHooks)
	calls := 0
	agents.RegisterHooks(agents.Hooks{
		HAServiceOperation: func(context.Context, *agents.Manager, string, func() (*agents.Record, error), bool) (*agents.Record, error) {
			t.Fatal("legacy HA hook triggered")
			return nil, nil
		},
		HAServiceOperationTyped: func(ctx context.Context, m *agents.Manager, id string, run func() (json.RawMessage, error), uninstall bool) (json.RawMessage, error) {
			calls++
			return f.s.ServiceOperationJSON(ctx, id, run, uninstall)
		},
	})
	key := f.r.DataDir() + "\x00member:secondary"
	_, release, ok := pairLocks.TryLock(f.ctx, key)
	if !ok {
		t.Fatal("member lease")
	}
	defer release()
	for _, operation := range []agents.DirectOperation{agents.RestartService, agents.UninstallService} {
		result, err := f.m.RunServiceOperation(f.ctx, "secondary", operation, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Success bool   `json:"success"`
			Status  int    `json:"http_status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(result, &body); err != nil {
			t.Fatal(err)
		}
		if body.Success || body.Status != 409 || body.Message != msgOperationBusy {
			t.Fatalf("busy member bypassed lease: %s", result)
		}
	}
	if calls != 2 {
		t.Fatalf("typed hook calls=%d", calls)
	}
}
