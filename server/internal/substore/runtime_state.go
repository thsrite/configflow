package substore

import (
	"bytes"
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

type runtimeState struct {
	State           *string `json:"state"`
	Error           string  `json:"error"`
	LastUsed        float64 `json:"last_used"`
	Fields          model.Fields
	Raw             model.Raw
	invalidLastUsed bool
}

// UnmarshalJSON retains unowned runtime-state fields using the shared codec.
func (r *runtimeState) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unchanged runtime-state fields.
func (r runtimeState) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

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
	if err := json.Unmarshal(raw, &state); err != nil {
		return &runtimeState{}, nil
	}
	var members struct {
		LastUsed json.RawMessage `json:"last_used"`
	}
	if jsondecode.Unmarshal(raw, &members) == nil && bytes.Equal(bytes.TrimSpace(members.LastUsed), []byte("null")) {
		state.invalidLastUsed = true
	}
	return &state, nil
}
func (r *runtimeState) empty() bool {
	data, err := model.Marshal(r)
	return err == nil && bytes.Equal(data, []byte("{}"))
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
		data, err := model.Marshal(*patch.State)
		if err != nil {
			return err
		}
		if err := model.SetMemberJSON(state, &state.Raw, "state", data); err != nil {
			return err
		}
	}
	if patch.Error != nil {
		data, err := model.Marshal(*patch.Error)
		if err != nil {
			return err
		}
		if err := model.SetMemberJSON(state, &state.Raw, "error", data); err != nil {
			return err
		}
	}
	if patch.LastUsed != nil {
		data, err := model.Marshal(*patch.LastUsed)
		if err != nil {
			return err
		}
		if err := model.SetMemberJSON(state, &state.Raw, "last_used", data); err != nil {
			return err
		}
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
	file, err := os.CreateTemp(root, ".state.*.tmp")
	if err != nil {
		return fmt.Errorf("stage Sub-Store runtime state: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write Sub-Store runtime state: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync Sub-Store runtime state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Sub-Store runtime state: %w", err)
	}
	if beforeReplace != nil {
		beforeReplace()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(root, "state.json")); err != nil {
		return fmt.Errorf("commit Sub-Store runtime state: %w", err)
	}
	return nil
}
