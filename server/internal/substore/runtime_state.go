package substore

import (
	"bytes"
	"configflow/internal/fsx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RuntimeStateError identifies corrupt state without exposing its stored values.
type RuntimeStateError struct{ Field string }

// Error returns a stable public diagnostic for the invalid schema field.
func (e *RuntimeStateError) Error() string {
	if e.Field == "last_used" {
		return msgRuntimeStateLastUsed
	}
	return msgRuntimeStateShape
}

// runtimeState is state.json: the members the runtime wrote (Python merges
// each write into the previous file). error is kept as stored (Python wrote
// it as given); invalidLastUsed: last_used is null or not a number, which
// stops idle handling (RuntimeStateError); stored: the file had a member.
type runtimeState struct {
	State           *string         `json:"state,omitzero"`
	Error           json.RawMessage `json:"error,omitzero"`
	LastUsed        *float64        `json:"last_used,omitzero"`
	invalidLastUsed bool
	stored          bool
}

// errorText is the stored error as text ("" when absent or not text).
func (r *runtimeState) errorText() string {
	var text model.String
	if len(r.Error) == 0 || json.Unmarshal(r.Error, &text) != nil {
		return ""
	}
	return string(text)
}

type runtimePatch struct {
	State    *string  `json:"state"`
	Error    *string  `json:"error"`
	LastUsed *float64 `json:"last_used"`
}

// RuntimeStatus describes the observed lifecycle without changing it.
type RuntimeStatus struct {
	Managed     bool    `json:"managed"`
	Mode        string  `json:"mode"`
	State       string  `json:"state"`
	IdleSeconds float64 `json:"idle_seconds"`
	Error       string  `json:"error"`
}

func (r *Runtime) loadState() (*runtimeState, error) {
	if err := r.checkTempDirectory(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(r.Root(), "state.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			var directory *RuntimeDirectoryError
			failure := r.directoryFailure(err)
			if errors.As(failure, &directory) {
				return nil, failure
			}
		}
		return &runtimeState{}, nil
	}
	var raw json.RawMessage
	if jsonresp.DecodeStored(data, &raw) != nil {
		return &runtimeState{}, nil
	}
	if raw = bytes.TrimSpace(raw); len(raw) == 0 || raw[0] != '{' {
		return nil, &RuntimeStateError{}
	}
	var state runtimeState
	issues, err := model.DecodeStored(raw, &state, "")
	if err != nil {
		return &runtimeState{}, nil
	}
	var members map[string]json.RawMessage
	if jsondecode.Unmarshal(raw, &members) == nil {
		state.stored = len(members) > 0
		if lastUsed, ok := members["last_used"]; ok && bytes.Equal(bytes.TrimSpace(lastUsed), []byte("null")) {
			state.invalidLastUsed = true
		}
	}
	if model.IssueUnder(issues, "last_used") {
		state.invalidLastUsed = true
	}
	return &state, nil
}

// empty reports a state file without members (Python: `if not state`).
func (r *runtimeState) empty() bool {
	return !r.stored && r.State == nil && r.Error == nil && r.LastUsed == nil
}

// writeState is called under the existing control lease. Rename is its commit.
func (r *Runtime) writeState(ctx context.Context, patch runtimePatch) error {
	return r.writeStateUsing(ctx, patch, nil)
}
func (r *Runtime) writeStateUsing(ctx context.Context, patch runtimePatch, beforeReplace func()) (failure error) {
	defer func() { failure = r.directoryFailure(failure) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := r.loadState()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if patch.State != nil {
		state.State = new(*patch.State)
	}
	if patch.Error != nil {
		data, err := model.Marshal(*patch.Error)
		if err != nil {
			return err
		}
		state.Error = data
	}
	if patch.LastUsed != nil {
		state.LastUsed = new(*patch.LastUsed)
		state.invalidLastUsed = false
	}
	data, err := model.Marshal(state)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := r.Root()
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("create Sub-Store runtime directory: %w", err)
	}
	path := filepath.Join(root, "state.json")
	temp, err := fsx.Stage(ctx, path, data, fsx.Options{Pattern: ".state.*.tmp"})
	if err != nil {
		return fmt.Errorf("stage Sub-Store runtime state: %w", err)
	}
	if beforeReplace != nil {
		beforeReplace()
	}
	if err := temp.Commit(ctx, path); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("commit Sub-Store runtime state: %w", err)
	}
	return nil
}
