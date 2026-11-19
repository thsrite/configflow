package upgrades

import (
	"configflow/internal/agents"
	"configflow/internal/repo"
	"errors"
	"fmt"
)

// ValidationError is a user-facing upgrade refusal.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func valueError(message string) error    { return &ValidationError{Message: message} }

// CorruptStateError refuses damaged persisted operation evidence without clearing it.
// Administrators must repair the stored field before another operation can proceed.
type CorruptStateError struct{ Field string }

func (e *CorruptStateError) Error() string { return fmt.Sprintf(msgCorruptState, e.Field) }

// ErrInvalidReport means a legacy Agent report has no valid ownership proof.
var ErrInvalidReport = errors.New("Invalid legacy upgrade report")

// IsValueError retains the existing route's refusal classification.
func IsValueError(err error) bool {
	var refusal *ValidationError
	var conflict *agents.DeploymentConflict
	var agentValue *agents.ValueError
	return errors.As(err, &refusal) || errors.As(err, &conflict) || errors.As(err, &agentValue) || errors.Is(err, agents.ErrHeaderEncoding) || repo.IsValueError(err)
}
