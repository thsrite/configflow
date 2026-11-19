package installation

import (
	"configflow/internal/agents"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"errors"
)

// Wire supplies the registry's capability-scoped installation hooks. Only this
// owner validates native JSON and returns no global registration key.
func Wire() {
	agents.RegisterHooks(agents.Hooks{

		InstallationProfileTyped: func(ctx context.Context, data json.RawMessage, id, token, fallback string) (string, json.RawMessage, error) {
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return "", nil, err
			}
			s := Service{Repository: r}
			profile, updated, err := s.RegistrationProfile(ctx, id, token, data, fallback)
			if err != nil {
				return "", nil, hookError(ctx, &s, err)
			}
			return profile, updated, nil
		},
		InstallationRegisterTyped: func(ctx context.Context, m *agents.Manager, id, token string, data json.RawMessage, existing string) (json.RawMessage, error) {
			s := Service{Repository: m.Repository(), Manager: m}
			result, err := s.Register(ctx, id, token, data, existing)
			if err != nil {
				return nil, hookError(ctx, &s, err)
			}
			return result, nil
		},
	})
}
func asString(v any) string { s, _ := v.(string); return s }
func hookError(ctx context.Context, s *Service, err error) error {
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
