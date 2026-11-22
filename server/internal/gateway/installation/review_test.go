package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/rules"
)

// A22 review F2: Python's enrollment refuses when any other Agent already holds
// the exact name/address target, regardless of list order.
func TestEnrollRefusesEarlierDuplicateTarget(t *testing.T) {
	f := newFixture(t)
	proofs := map[string]json.RawMessage{}
	for _, service := range services {
		o := object(f.register(t, service))
		proofs[service] = raw(map[string]json.RawMessage{"id": o["id"], "token": o["token"]})
	}
	// An administrator edit can give a second record the same name and host;
	// placing it first is the order a last-wins index cannot see.
	err := f.s.Repository.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		duplicate := model.Agent{
			ID:          "agent_duplicate",
			Name:        f.r.Name + "-mihomo",
			Host:        f.r.Network.Address,
			Port:        9000,
			Token:       "synthetic-duplicate-token",
			ServiceType: "mihomo",
			ProfileID:   "default",
		}
		sys.Agents = append([]model.Agent{duplicate}, sys.Agents...)
		return nil
	})
	must(t, err)
	before := document(t, f)
	record := recordBytes(t, f)
	_, err = f.s.Enroll(f.ctx, f.r.ID, f.r.Token, raw(map[string]any{"agents": proofs}))
	refusal(t, err, 409, msgDuplicate)
	if !bytes.Equal(before, document(t, f)) || !bytes.Equal(record, recordBytes(t, f)) {
		t.Fatal("refused enrollment wrote")
	}
}

// A22 review F4: Python stores str(exc) for every ValueError raised while
// preparing a core (strategy references, base settings, remote rule content,
// profile validation), so the wizard names the configuration to fix. Only
// other exceptions get the generic "check the device" text.
func TestPublicationFailureNamesPythonValueErrors(t *testing.T) {
	const text = "策略组「Proxy」引用了不存在的节点或策略组：Missing"
	reference := fmt.Errorf("prepare mihomo: %w", &rules.StrategyReferenceError{Message: text})
	var public *Error
	var prep *deploy.PreparationError
	if errors.As(reference, &public) || errors.As(reference, &prep) {
		t.Fatal("control: the error is already an installation/preparation error")
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"strategy reference", &rules.StrategyReferenceError{Message: text}, text},
		{"installation refusal", failure(msgConfigChanged, 409), msgConfigChanged},
		{"transport", errors.New("synthetic transport failure"), msgPublishFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failureMessage(tt.err); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
	if got := failureMessage(reference); got != reference.Error() {
		t.Fatalf("wrapped ValueError hidden: %q", got)
	}
}
