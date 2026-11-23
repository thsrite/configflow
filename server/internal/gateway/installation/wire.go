package installation

import (
	"configflow/internal/agents"
	"context"
	"errors"
)

// WrapError redacts secrets and maps installation refusals to *agents.InstallationError.
func (s *Service) WrapError(ctx context.Context, err error) error {
	err = s.safeError(ctx, err)
	var e *Error
	if errors.As(err, &e) {
		return &agents.InstallationError{Message: e.Message, Status: e.Status}
	}
	return err
}

// PublicError retains a refusal's status and cause while removing stored secrets.
func (s *Service) PublicError(ctx context.Context, err error) error {
	return s.safeError(ctx, publicOwnerError(err))
}
