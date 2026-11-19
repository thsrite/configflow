package agents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// RegistrationTTL matches the gateway installation session's 24-hour lifetime.
const RegistrationTTL = 24 * time.Hour

// MaxRegistrationSessions bounds outstanding, unconsumed installation identities.
const MaxRegistrationSessions = 100

// ErrRegistrationAuthorization rejects absent, expired, used or mismatched proofs.
var ErrRegistrationAuthorization = errors.New("agent registration authorization required")

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
	if len(identities) > MaxRegistrationSessions {
		return nil, fmt.Errorf("registration batch exceeds %d identities", MaxRegistrationSessions)
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
		// All sessions have the same TTL, so expiry orders issuance times.
		// Stable ordering preserves insertion order for same-second batches.
		sort.SliceStable(active, func(i, j int) bool { return active[i].Expires < active[j].Expires })
		if excess := len(active) + len(sessions) - MaxRegistrationSessions; excess > 0 {
			active = active[excess:]
		}
		system.RegistrationSessions = append(active, sessions...)
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// registrationClaim is an Agent's exact registration identity. Decoding old
// external records belongs to registration_legacy.go, not to admission policy.
type registrationClaim struct {
	Name, Host, ServiceType, DeploymentMethod, ProfileID string
	Port                                                 model.Int
	invalid                                              bool
}

func (m *Manager) registrationProfileExists(ctx context.Context, id string) error {
	if err := repo.ValidateProfileID(id); err != nil {
		return err
	}
	return m.repository.View(ctx, func(document *model.Document) error {
		for _, profile := range document.Profiles {
			if profile.Key() == id {
				return nil
			}
		}
		return &repo.Error{Kind: repo.ErrNotFound, Message: id}
	})
}

func (m *Manager) matchesRegistrationClaim(ctx context.Context, system *model.System, claim registrationClaim, admission RegistrationAuthorization) bool {
	if admission.Credential == "" {
		return false
	}
	probe := &model.System{RegistrationSessions: append([]model.AgentRegistrationSession(nil), system.RegistrationSessions...)}
	admission.AllowNew = false
	_, err := m.admitRegistrationClaim(ctx, probe, claim, admission, nil)
	return err == nil
}

// admissionSessionIdentity validates original stored proof token types. Shared
// text coercion must not turn numeric identity members into a capability.
func admissionSessionIdentity(session model.AgentRegistrationSession) bool {
	for _, key := range []string{"hash", "name", "host", "service_type", "deployment_method", "profile_id"} {
		raw, present, err := session.MemberJSON(key)
		if err != nil {
			return false
		}
		if !present {
			if key == "hash" {
				return false
			}
			continue
		}
		var text *string
		if json.Unmarshal(raw, &text) != nil || text == nil {
			return false
		}
		if key == "hash" && (*text == "" || *text != session.Hash) {
			return false
		}
	}
	for _, key := range []string{"port", "expires"} {
		raw, present, err := session.MemberJSON(key)
		if err != nil || !present {
			return false
		}
		var value *model.Int
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return false
		}
	}
	return true
}

// registrationSessionPortMatches requires a JSON number at the stored proof
// boundary. model.Int's compatibility string decode is not ownership evidence.
func registrationSessionPortMatches(session model.AgentRegistrationSession, port model.Int) bool {
	raw, present, err := session.MemberJSON("port")
	if err != nil || !present {
		return false
	}
	var number *float64
	if json.Unmarshal(raw, &number) != nil || number == nil {
		return false
	}
	var stored model.Int
	return json.Unmarshal(raw, &stored) == nil && stored == port
}

// admitRegistrationClaim changes only the caller's system working copy. Its
// success and the Agent insert/update commit together in the owning transaction.
func (m *Manager) admitRegistrationClaim(ctx context.Context, system *model.System, claim registrationClaim, admission RegistrationAuthorization, validate func() error) (string, error) {
	authorized := func() (string, error) {
		if validate != nil {
			return "", validate()
		}
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	now := lifecycle.ClockFrom(ctx).Now().Unix()
	system.RegistrationSessions = activeRegistrationSessions(system.RegistrationSessions, now)
	host := admission.ReportedHost
	if host == "" {
		host = claim.Host
	}
	digest := credentialDigest(admission.Credential)
	for i, session := range system.RegistrationSessions {
		if subtle.ConstantTimeCompare([]byte(digest), []byte(session.Hash)) != 1 {
			continue
		}
		if admission.Credential == "" || claim.invalid || !admissionSessionIdentity(session) || !registrationSessionPortMatches(session, claim.Port) || session.Name != claim.Name ||
			session.ServiceType != claim.ServiceType || session.DeploymentMethod != claim.DeploymentMethod || session.Port != claim.Port ||
			session.Host != "" && session.Host != host || claim.ProfileID != "" && claim.ProfileID != session.ProfileID {
			if admission.AllowNew {
				return authorized()
			}
			return "", ErrRegistrationAuthorization
		}
		if err := m.registrationProfileExists(ctx, session.ProfileID); err != nil {
			if admission.AllowNew {
				return authorized()
			}
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if validate != nil {
			if err := validate(); err != nil {
				return "", err
			}
		}
		system.RegistrationSessions = append(system.RegistrationSessions[:i], system.RegistrationSessions[i+1:]...)
		return session.ProfileID, nil
	}
	if admission.AllowNew {
		return authorized()
	}
	return "", ErrRegistrationAuthorization
}
