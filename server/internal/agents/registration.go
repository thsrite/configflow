package agents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// RegistrationTTL matches the gateway installation session's 24-hour lifetime.
const RegistrationTTL = 24 * time.Hour

// MaxRegistrationSessions bounds outstanding, unconsumed installation identities.
const MaxRegistrationSessions = 100

// ErrRegistrationAuthorization rejects absent, expired, used or mismatched proofs.
var ErrRegistrationAuthorization = errors.New("agent registration authorization required")

// ErrRegistrationSessionsFull leaves unexpired capabilities intact at capacity.
var ErrRegistrationSessionsFull = errors.New("agent registration session capacity reached")

// RegistrationAuthorization is supplied by the HTTP authentication boundary,
// never decoded from registration JSON. Existing Agent tokens still prove ownership.
type RegistrationAuthorization struct {
	AllowNew   bool
	Credential string
	// ReportedHost retains the client claim before legacy Docker/loopback host fallback.
	ReportedHost string
}

func credentialDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func activeRegistrationSessions(sessions []model.AgentRegistrationSession, now int64) []model.AgentRegistrationSession {
	active := make([]model.AgentRegistrationSession, 0, len(sessions))
	for _, session := range sessions {
		if int64(session.Expires) > now {
			active = append(active, session)
		}
	}
	return active
}

// CreateRegistrationCredentials issues one independent proof per exact service
// identity. A multi-service installation never shares a reusable bearer proof.
// Host may be empty when the panel cannot know the future device's address.
func (m *Manager) CreateRegistrationCredentials(ctx context.Context, identities []model.AgentRegistrationSession) (map[string]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokens := make(map[string]string, len(identities))
	sessions := make([]model.AgentRegistrationSession, len(identities))
	now := lifecycle.ClockFrom(ctx).Now()
	profile, err := m.defaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	for i, identity := range identities {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			return nil, fmt.Errorf("generate registration credential: %w", err)
		}
		token := base64.RawURLEncoding.EncodeToString(secret[:])
		tokens[identity.ServiceType] = token
		identity.Hash = credentialDigest(token)
		identity.ProfileID = profile
		identity.Expires = model.Int(now.Add(RegistrationTTL).Unix())
		sessions[i] = identity
	}
	// Session bookkeeping is private control state, like gateway session files;
	// it must not invalidate user configuration revisions or publication previews.
	err = m.repository.Update(ctx, func(ctx context.Context, document *model.Document) error {
		system := &document.System
		if err := ctx.Err(); err != nil {
			return err
		}
		active := activeRegistrationSessions(system.RegistrationSessions, now.Unix())
		if len(active)+len(sessions) > MaxRegistrationSessions {
			return ErrRegistrationSessionsFull
		}
		system.RegistrationSessions = append(active, sessions...)
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// RegisterAuthorizedAgent enforces the login-enabled first-registration policy
// inside the registry transaction, after the existing-identity ownership check.
func (m *Manager) RegisterAuthorizedAgent(ctx context.Context, data *Record, existingToken any, admission RegistrationAuthorization) (*Result, error) {
	return m.registerAgent(ctx, data, existingToken, nil, false, &admission)
}

// proofMatches reports whether admission carries a live proof for this exact
// identity, without consuming it or touching the transaction's system copy.
func (m *Manager) proofMatches(ctx context.Context, system *model.System, data *Record, admission *RegistrationAuthorization) bool {
	if admission.Credential == "" {
		return false
	}
	probe := &model.System{RegistrationSessions: append([]model.AgentRegistrationSession(nil), system.RegistrationSessions...)}
	strict := *admission
	strict.AllowNew = false
	// With AllowNew false, admitRegistration succeeds only on an exact match.
	return m.admitRegistration(ctx, probe, data.DeepCopy(), &strict) == nil
}

func (m *Manager) admitRegistration(ctx context.Context, system *model.System, data *Record, admission *RegistrationAuthorization) error {
	now := lifecycle.ClockFrom(ctx).Now().Unix()
	system.RegistrationSessions = activeRegistrationSessions(system.RegistrationSessions, now)
	host := admission.ReportedHost
	if host == "" {
		host = data.GetString("host")
	}
	digest := credentialDigest(admission.Credential)
	for i, session := range system.RegistrationSessions {
		if subtle.ConstantTimeCompare([]byte(digest), []byte(session.Hash)) != 1 {
			continue
		}
		if admission.Credential == "" || session.Name != data.GetString("name") ||
			session.ServiceType != data.GetOr("service_type", "mihomo") || session.DeploymentMethod != data.GetString("deployment_method") ||
			!jsonx.Equal(int64(session.Port), data.GetOr("port", 8080)) || session.Host != "" && session.Host != host ||
			data.GetString("profile_id") != "" && data.GetString("profile_id") != session.ProfileID {
			if admission.AllowNew {
				return nil
			}
			return ErrRegistrationAuthorization
		}
		if _, err := m.repository.ProfileMetadata(ctx, session.ProfileID); err != nil {
			if admission.AllowNew {
				return nil
			}
			return err
		}
		data.Set("profile_id", session.ProfileID)
		system.RegistrationSessions = append(system.RegistrationSessions[:i], system.RegistrationSessions[i+1:]...)
		return nil
	}
	if admission.AllowNew {
		return nil
	}
	return ErrRegistrationAuthorization
}
