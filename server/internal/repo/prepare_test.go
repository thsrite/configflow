package repo

import (
	"context"
	"testing"
)

func TestPrepareConfigDefersAgentInitialization(t *testing.T) {
	r := newRepo(t)
	ctx := WithScope(WithRepository(context.Background(), r))
	defer ResetConfigContext(ctx)
	initializations := 0
	previous := SetHooks(Hooks{InitAgentManager: func() { initializations++ }})
	defer SetHooks(previous)
	prepared, err := PrepareConfig(ctx)
	if err != nil || prepared == nil || initializations != 0 {
		t.Fatalf("prepare: document=%v, init=%d, err=%v", prepared != nil, initializations, err)
	}
	loaded, err := LoadConfig(ctx)
	if err != nil || loaded == nil || initializations != 1 {
		t.Fatalf("legacy load: document=%v, init=%d, err=%v", loaded != nil, initializations, err)
	}
}
