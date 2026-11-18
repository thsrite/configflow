package installation

import (
	"configflow/internal/agents"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"errors"
)

// Wire supplies the registry's capability-scoped installation hooks. Only this
// adapter sees the legacy callback argument types; no global key is returned.
func Wire() {
	agents.RegisterHooks(agents.Hooks{
		InstallationProfile: func(ctx context.Context, data *agents.Record, id, token any, fallback string) (any, error) {
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return nil, err
			}
			s := Service{Repository: r}
			dataRaw, err := json.Marshal(data)
			if err != nil {
				return nil, err
			}
			profile, updated, err := s.RegistrationProfile(ctx, asString(id), asString(token), dataRaw, fallback)
			if err != nil {
				return nil, hookError(ctx, &s, err)
			}
			next, err := toLegacy(updated)
			if err != nil {
				return nil, err
			}
			data.Update(next)
			return profile, nil
		},
		InstallationRegister: func(ctx context.Context, m *agents.Manager, id, token any, data *agents.Record, existing any) (*agents.Result, error) {
			s := Service{Repository: m.Repository(), Manager: m}
			b, err := json.Marshal(data)
			if err != nil {
				return nil, err
			}
			result, err := s.Register(ctx, asString(id), asString(token), b, asString(existing))
			if err != nil {
				return nil, hookError(ctx, &s, err)
			}
			return toLegacy(result)
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
