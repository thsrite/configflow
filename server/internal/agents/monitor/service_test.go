package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestServiceSetHARulesTwoPhaseAndFailClosed(t *testing.T) {
	svc := New(t.TempDir())

	// Negative control: nil HARules MUST fail closed with ErrHARulesNotPorted.
	if _, _, err := svc.PairRules(context.Background(), nil, "a", "POST"); !errors.Is(err, ErrHARulesNotPorted) {
		t.Fatalf("PairRules without HARules = %v, want ErrHARulesNotPorted", err)
	}

	calls := 0
	fn := func(context.Context, Registry, string, string) (json.RawMessage, int, error) {
		calls++
		return json.RawMessage(`{"success":true}`), 200, nil
	}
	if err := svc.SetHARules(fn); err != nil {
		t.Fatalf("first SetHARules: %v", err)
	}
	if err := svc.SetHARules(fn); !errors.Is(err, ErrHARulesAlreadySet) {
		t.Fatalf("second SetHARules = %v, want ErrHARulesAlreadySet", err)
	}
	if raw, code, err := svc.PairRules(context.Background(), nil, "a", "POST"); err != nil || code != 200 || calls != 1 || string(raw) != `{"success":true}` {
		t.Fatalf("PairRules after SetHARules: raw=%s code=%d err=%v calls=%d", raw, code, err, calls)
	}

	svc2 := New(t.TempDir())
	svc2.MarkStarted()
	if err := svc2.SetHARules(fn); !errors.Is(err, ErrServiceStarted) {
		t.Fatalf("SetHARules after MarkStarted = %v, want ErrServiceStarted", err)
	}
}
