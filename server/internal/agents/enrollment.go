package agents

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// ErrRegistrationProfileType rejects nontext machine-generated profile bindings.
// It also matches repo.ErrValidation; the HTTP boundary reports this class as 400.
var ErrRegistrationProfileType = &repo.Error{Kind: repo.ErrValidation, Message: "Invalid profile id"}

// ErrRegistrationVersion rejects an invalid machine version after authorization.
var ErrRegistrationVersion = &repo.Error{Kind: repo.ErrValidation, Message: "Invalid Agent version"}
var machineVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// ValidMachineVersion accepts the shared ASCII Agent version grammar.
func ValidMachineVersion(text string) bool { return machineVersionPattern.MatchString(text) }

// normalizeRegistrationVersion applies the default version to a reported
// null and refuses a version that is not valid machine text.
func normalizeRegistrationVersion(input *RegistrationInput) error {
	raw := bytes.TrimSpace(input.version)
	if len(raw) == 0 || string(raw) == "null" {
		input.record.Version = "1.0.0"
		return nil
	}
	var text string
	if raw[0] != '"' || json.Unmarshal(raw, &text) != nil || !ValidMachineVersion(text) {
		return ErrRegistrationVersion
	}
	input.record.Version = text
	return nil
}

// RegistrationInput is an exact-key request DTO. Only the registry's public
// identity and reported version fields can enter a newly registered record.
type RegistrationInput struct {
	record  model.Agent
	claim   registrationClaim
	profile string
	// version is the reported version as received; deploymentMethod reports
	// whether the request named a deployment method.
	version          json.RawMessage
	deploymentMethod bool
}

// DecodeRegistrationInput decodes one registration request. Members other
// than the identity and version are ignored; identity members must have
// their JSON types, profile bindings require text.
func DecodeRegistrationInput(data json.RawMessage) (RegistrationInput, error) {
	input := RegistrationInput{claim: decodeRegistrationClaim(data)}
	if err := requireJSONObject(data); err != nil {
		return input, err
	}
	var members model.OrderedMap[json.RawMessage]
	if err := json.Unmarshal(data, &members); err != nil {
		return input, err
	}
	for _, field := range []struct{ name, fallback string }{
		{"name", `"Unnamed Agent"`}, {"host", `""`}, {"port", `8080`},
		{"service_type", `"mihomo"`}, {"deployment_method", ``},
	} {
		raw, present := members.Get(field.name)
		if !present {
			raw = json.RawMessage(field.fallback)
		}
		if len(raw) == 0 {
			continue
		}
		// Agents report these members; a value of another type reads as
		// stored data does (its zero value), as the record kept it before.
		if _, _, err := model.DecodeStoredMember(&input.record, field.name, raw, field.name); err != nil {
			return input, err
		}
		input.deploymentMethod = input.deploymentMethod || field.name == "deployment_method"
	}
	input.version = json.RawMessage(`"1.0.0"`)
	if raw, present := members.Get("version"); present {
		input.version = raw
	}
	_ = json.Unmarshal(input.version, &input.record.Version)
	if raw, present := members.Get("profile_id"); present && string(bytes.TrimSpace(raw)) != "null" {
		var profile string
		if bytes.TrimSpace(raw)[0] != '"' || json.Unmarshal(raw, &profile) != nil {
			return input, ErrRegistrationProfileType
		}
		input.profile = profile
		if profile != "" {
			if err := repo.ValidateProfileID(profile); err != nil {
				return input, err
			}
		}
	}
	return input, nil
}

// RegistrationOptions contains trusted authentication and installation data;
// these values must never be decoded from an Agent's registration JSON.
type RegistrationOptions struct {
	ExistingToken      string
	InstallationID     string
	ReplayInstallation bool
	Authorization      *RegistrationAuthorization
}

// RegistrationResult returns a new token once, or on an authorized exact
// installation replay. An ordinary re-registration never returns its token.
type RegistrationResult struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	IsNew  bool    `json:"is_new"`
	Token  *string `json:"token,omitempty"`
}

func tokenMatches(offered, expected string) bool {
	return expected != "" && isASCII(offered) && isASCII(expected) && subtle.ConstantTimeCompare([]byte(offered), []byte(expected)) == 1
}

// RegisterIdentity owns registration, one-use proof consumption and record
// replacement in the same repository transaction. No capability plaintext is
// stored; an anonymous wrong token remains an opaque authorization refusal.
func (m *Manager) RegisterIdentity(ctx context.Context, input RegistrationInput, options RegistrationOptions) (RegistrationResult, error) {
	ctx = lifecycle.Detach(ctx)
	result := RegistrationResult{}
	err := m.editRegistry(ctx, input.profile, func(tx context.Context, system *model.System) error {
		for i := range system.Agents {
			a := &system.Agents[i]
			if input.record.Name != a.Name || input.record.Host != a.Host || !agentIDProven(a) {
				continue
			}
			return m.refreshRegistration(tx, system, a, input, options, &result)
		}
		profile := input.profile
		if admission := options.Authorization; admission != nil {
			bound, err := m.admitRegistrationClaim(tx, system, input.claim, *admission, func() error { return normalizeRegistrationVersion(&input) })
			if err != nil {
				return err
			}
			if bound != "" {
				profile = bound
			}
		}
		if options.Authorization == nil {
			if err := normalizeRegistrationVersion(&input); err != nil {
				return err
			}
		}
		if profile == "" {
			var err error
			profile, err = m.defaultProfileID(tx)
			if err != nil {
				return err
			}
		}
		var entropy [27]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return fmt.Errorf("generate Agent identity: %w", err)
		}
		id := fmt.Sprintf("agent_%d_%s", clock().UnixMilli(), hex.EncodeToString(entropy[:3]))
		token := base64.RawURLEncoding.EncodeToString(entropy[3:])
		a := model.Agent{}
		setRegistrationMembers(&a, &input, id, token, profile, false)
		if options.InstallationID != "" {
			a.GatewayInstallationID = new(options.InstallationID)
		}
		system.Agents = append(system.Agents, a)
		result = RegistrationResult{ID: id, Status: "online", IsNew: true, Token: new(token)}
		return tx.Err()
	})
	return result, err
}

func (m *Manager) refreshRegistration(ctx context.Context, system *model.System, a *model.Agent, input RegistrationInput, options RegistrationOptions, result *RegistrationResult) error {
	marker := ""
	if a.GatewayInstallationID != nil {
		marker = *a.GatewayInstallationID
	}
	owned := options.ReplayInstallation && options.InstallationID != "" && marker == options.InstallationID
	storedToken := a.Token
	offered := options.ExistingToken
	reported := &input.record
	if owned {
		if reported.Name != a.Name || reported.Host != a.Host || reported.Port != a.Port || reported.ServiceType != a.ServiceType ||
			reported.DeploymentMethod != a.DeploymentMethod || input.profile != a.ProfileID {
			return &DeploymentConflict{Message: installationIdentityChanged}
		}
		offered = storedToken
	}
	if !tokenMatches(offered, storedToken) {
		if admission := options.Authorization; admission != nil && !admission.AllowNew && !m.matchesRegistrationClaim(ctx, system, input.claim, *admission) {
			return ErrRegistrationAuthorization
		}
		return ErrUnauthorized
	}
	if admission := options.Authorization; admission != nil && admission.Credential != "" {
		optional := *admission
		optional.AllowNew = true
		if _, err := m.admitRegistrationClaim(ctx, system, input.claim, optional, func() error { return normalizeRegistrationVersion(&input) }); err != nil {
			return err
		}
	}
	if admission := options.Authorization; admission == nil || admission.Credential == "" {
		if err := normalizeRegistrationVersion(&input); err != nil {
			return err
		}
	}
	if options.InstallationID != "" && marker == options.InstallationID && input.profile != a.ProfileID {
		return &DeploymentConflict{Message: msgInstallationProfileChanged}
	}
	profile := input.profile
	current, profileErr := m.agentProfileID(ctx, a)
	if profile == "" {
		if profileErr != nil {
			return profileErr
		}
		profile = current
	} else if (profileErr != nil || profile != current) && (upgradeBusyModel(a) || unfinishedDeploymentModel(a)) {
		return &DeploymentConflict{Message: msgRegistrationDeploymentBusy}
	}
	changes := reported.ServiceType != a.ServiceType || reported.Port != a.Port || input.profile != "" && input.profile != a.ProfileID
	if changes && dnsFollowBusyModel(system.Agents, a.ID) {
		return &DeploymentConflict{Message: msgRegistrationDNSFollowBusy}
	}
	if changes && (HAEnabledModel(a) || HAActiveModel(a)) {
		return &DeploymentConflict{Message: OxidnsHALocked}
	}
	if changes && StateRestored(a) {
		return &DeploymentConflict{Message: StateRestoredMessage}
	}
	if input.profile != "" {
		profile = input.profile
	}
	setRegistrationMembers(a, &input, a.ID, storedToken, profile, true)
	*result = RegistrationResult{ID: a.ID, Status: "online"}
	if owned {
		result.Token = new(storedToken)
	}
	return ctx.Err()
}

// setRegistrationMembers rebuilds a registered Agent from register_agent's
// fixed member list. On a refresh the panel-owned members listed below are
// kept; every other member (Docker HA enrollment marker, runtime metrics) is
// dropped as in Python: a retained enrollment marker would let that
// installation credential replay this Agent's token after the device itself
// registered.
func setRegistrationMembers(a *model.Agent, input *RegistrationInput, id, token, profile string, refresh bool) {
	original := *a
	reported := &input.record
	now := nowISO()
	next := model.Agent{
		ID: id, Name: reported.Name, Host: reported.Host, Port: reported.Port, Token: token, ServiceType: reported.ServiceType,
		ProfileID: profile, DeploymentMethod: reported.DeploymentMethod, Status: "online", LastHeartbeat: now,
		Version: reported.Version, ConfigVersion: "0", Enabled: new(true), CreatedAt: now,
	}
	if !input.deploymentMethod {
		next.DeploymentMethod = "unknown"
		if refresh && original.DeploymentMethod != "" {
			next.DeploymentMethod = original.DeploymentMethod
		}
	}
	if !refresh {
		*a = next
		return
	}
	if original.ConfigVersion != "" {
		next.ConfigVersion = original.ConfigVersion
	}
	if original.CreatedAt != "" {
		next.CreatedAt = original.CreatedAt
	}
	next.UpdatedAt = new(nowISO())
	next.Deployments = original.Deployments
	next.LatestDeployment = original.LatestDeployment
	next.LatestUpgrade = original.LatestUpgrade
	next.DomainDiscoveryEnabled = original.DomainDiscoveryEnabled
	if next.DomainDiscoveryEnabled == nil {
		next.DomainDiscoveryEnabled = new(false)
	}
	next.TrafficHistoryEnabled = original.TrafficHistoryEnabled
	next.TrafficHistorySince = original.TrafficHistorySince
	next.ConfigRevision = original.ConfigRevision
	next.PushedAt = original.PushedAt
	next.AppliedContent = original.AppliedContent
	next.DNSFollow = original.DNSFollow
	next.DNSFollowState = original.DNSFollowState
	next.DNSFollowJob = original.DNSFollowJob
	next.RuleSync = original.RuleSync
	next.OxiDNSHA = original.OxiDNSHA
	next.OxiDNSHAState = original.OxiDNSHAState
	next.OxiDNSHAPublication = original.OxiDNSHAPublication
	next.IPv6VerificationSettings = original.IPv6VerificationSettings
	next.IPv6Verifications = original.IPv6Verifications
	next.StateRestoredAt = original.StateRestoredAt
	// Python keeps the trusted installation marker only when it is truthy.
	if original.GatewayInstallationID != nil && *original.GatewayInstallationID != "" {
		next.GatewayInstallationID = original.GatewayInstallationID
	}
	*a = next
}
