package dnsha

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

type countingPairPublisher struct {
	*Service
	serviceOpCalls int
}

func (c *countingPairPublisher) PairDeployment(ctx context.Context, id, did string) (*model.Deployment, error) {
	return c.Service.PairDeployment(ctx, id, did)
}

func (c *countingPairPublisher) ActivatePair(ctx context.Context, id, did string) (*model.Deployment, error) {
	return c.Service.ActivatePair(ctx, id, did)
}

func (c *countingPairPublisher) ServiceOperationJSON(ctx context.Context, id string, op func() (json.RawMessage, error), uninstall bool) (json.RawMessage, error) {
	c.serviceOpCalls++
	return c.Service.ServiceOperationJSON(ctx, id, op, uninstall)
}

func TestN810NativeServiceOperationRespectsBusyMemberLease(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	m, err := agents.New(f.ctx, f.r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.SetClient(f.client)
	counted := &countingPairPublisher{Service: New(m, Options{})}
	if err := m.SetPairPublisher(counted); err != nil {
		t.Fatal(err)
	}
	key := f.r.DataDir() + "\x00member:secondary"
	_, release, ok := pairLocks.TryLock(f.ctx, key)
	if !ok {
		t.Fatal("member lease")
	}
	defer release()
	for _, operation := range []agents.DirectOperation{agents.RestartService, agents.UninstallService} {
		result, err := m.RunServiceOperation(f.ctx, "secondary", operation, nil, nil)
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
	if counted.serviceOpCalls != 2 {
		t.Fatalf("typed hook calls=%d", counted.serviceOpCalls)
	}
}
