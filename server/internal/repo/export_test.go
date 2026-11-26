package repo

import (
	"configflow/internal/model"
	"encoding/json"
	"errors"
	"path/filepath"
)

// AcquireFileForTest exposes admission without a second write operation.
var AcquireFileForTest = acquireFile

// StageFileForTest creates the real fsynced temporary file for commit tests.
var StageFileForTest = writeTemp

// WriteAtomicForTest exposes per-call filesystem seams without global hooks.
var WriteAtomicForTest = writeAtomicUsing

// RemoveSharedUsingForTest exposes the canonical delete commit with a real lease barrier.
var RemoveSharedUsingForTest = (*Repository).removeSharedFileUsing

// WithFileForTest acquires the production file lease for commit-boundary tests.
var WithFileForTest = withFile

// SetUpgradeRowHookForTest sets the store hook of the next storage upgrades.
func SetUpgradeRowHookForTest(hook func(i int) error) { upgradeRowHook = hook }

// UsagesOf returns the usages of a ProfileInUse error, or nil.
func UsagesOf(err error) []any {
	var e *Error
	if errors.As(err, &e) {
		return e.Usages
	}
	return nil
}

func (r *Repository) profileOperationLockPath(profileID string) (string, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return "", err
	}
	return filepath.Join(r.profilesDir, "."+profileID+".operation.lock"), nil
}

// AgentBindingRecord returns an Agent's public identity.
func AgentBindingRecord(agent model.Agent) (AgentBinding, error) {
	return agentBinding(&agent)
}

// AgentHistoryJSON returns an owned history member of a full typed Agent record.
func AgentHistoryJSON(agent model.Agent, field string) (json.RawMessage, error) {

	raw, _, err := recordMemberJSON(agent, field)
	return raw, err
}
