package agents

import (
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

func normalizeRegistrationVersion(record *model.Agent) error {
	raw, present, err := record.MemberJSON("version")
	if err != nil {
		return err
	}
	if !present || string(raw) == "null" {
		return record.SetJSON("version", json.RawMessage(`"1.0.0"`))
	}
	text, valid := storedText(raw, present, nil)
	if !valid || !ValidMachineVersion(text) {
		return ErrRegistrationVersion
	}
	return nil
}

// RegistrationInput is an exact-key request DTO. Only the registry's public
// identity and reported version fields can enter a newly registered record.
type RegistrationInput struct {
	record  model.Agent
	claim   registrationClaim
	profile string
}

// DecodeRegistrationInput decodes one registration request without interpreting
// unknown/case-alias keys as protocol fields. Profile bindings require text.
func DecodeRegistrationInput(data json.RawMessage) (RegistrationInput, error) {
	input := RegistrationInput{claim: decodeRegistrationClaim(data)}
	var members model.Fields
	if err := json.Unmarshal(data, &members); err != nil {
		return input, err
	}
	for _, field := range []struct{ name, fallback string }{
		{"name", `"Unnamed Agent"`}, {"host", `""`}, {"port", `8080`},
		{"service_type", `"mihomo"`}, {"profile_id", `null`}, {"deployment_method", ``}, {"version", `"1.0.0"`},
	} {
		raw, present := members.Get(field.name)
		if !present {
			raw = json.RawMessage(field.fallback)
		}
		if len(raw) == 0 {
			continue
		}
		if err := input.record.SetJSON(field.name, raw); err != nil {
			return input, err
		}
	}
	raw, present, err := input.record.MemberJSON("profile_id")
	if err != nil {
		return input, err
	}
	if !storedAbsent(raw, present, nil) {
		profile, valid := storedText(raw, present, nil)
		if !valid {
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
		name, nameOK := storedText(input.record.MemberJSON("name"))
		host, hostOK := storedText(input.record.MemberJSON("host"))
		for i := range system.Agents {
			a := &system.Agents[i]
			storedName, storedNameOK := storedText(a.MemberJSON("name"))
			storedHost, storedHostOK := storedText(a.MemberJSON("host"))
			if !nameOK || !hostOK || !storedNameOK || !storedHostOK || name != storedName || host != storedHost || !agentIDProven(a) {
				continue
			}
			return m.refreshRegistration(tx, system, a, input, options, &result)
		}
		profile := input.profile
		if admission := options.Authorization; admission != nil {
			bound, err := m.admitRegistrationClaim(tx, system, input.claim, *admission, func() error { return normalizeRegistrationVersion(&input.record) })
			if err != nil {
				return err
			}
			if bound != "" {
				profile = bound
			}
		}
		if options.Authorization == nil {
			if err := normalizeRegistrationVersion(&input.record); err != nil {
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
		// Stable field order also makes stored diffs readable. SetJSON records
		// explicit zero/null fields instead of relying on Go's zero omission.
		if err := setRegistrationMembers(&a, &input.record, id, token, profile, false); err != nil {
			return err
		}
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
	marker, markerOK := storedText(a.MemberJSON("gateway_installation_id"))
	owned := options.ReplayInstallation && options.InstallationID != "" && markerOK && marker == options.InstallationID
	storedToken, tokenOK := storedText(a.MemberJSON("token"))
	offered := options.ExistingToken
	if owned {
		for _, key := range []string{"name", "host", "port", "service_type", "deployment_method", "profile_id"} {
			raw, present, err := input.record.MemberJSON(key)
			if err != nil {
				return err
			}
			if !present {
				raw = json.RawMessage(`null`)
			}
			if !sameAgentMember(a, key, raw) {
				return &DeploymentConflict{Message: installationIdentityChanged}
			}
		}
		offered = storedToken
	}
	if !tokenOK || !tokenMatches(offered, storedToken) {
		if admission := options.Authorization; admission != nil && !admission.AllowNew && !m.matchesRegistrationClaim(ctx, system, input.claim, *admission) {
			return ErrRegistrationAuthorization
		}
		return &PermissionError{}
	}
	if admission := options.Authorization; admission != nil && admission.Credential != "" {
		optional := *admission
		optional.AllowNew = true
		if _, err := m.admitRegistrationClaim(ctx, system, input.claim, optional, func() error { return normalizeRegistrationVersion(&input.record) }); err != nil {
			return err
		}
	}
	if admission := options.Authorization; admission == nil || admission.Credential == "" {
		if err := normalizeRegistrationVersion(&input.record); err != nil {
			return err
		}
	}
	if options.InstallationID != "" && markerOK && marker == options.InstallationID {
		raw, present, err := input.record.MemberJSON("profile_id")
		if err != nil {
			return err
		}
		if !present {
			raw = json.RawMessage(`null`)
		}
		if !sameAgentMember(a, "profile_id", raw) {
			return &DeploymentConflict{Message: msgInstallationProfileChanged}
		}
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
	changes := false
	for _, key := range []string{"service_type", "port", "profile_id"} {
		if key == "profile_id" && input.profile == "" {
			continue
		}
		raw, _, err := input.record.MemberJSON(key)
		if err != nil {
			return err
		}
		changes = changes || !sameAgentMember(a, key, raw)
	}
	if changes && dnsFollowBusyModel(system.Agents, a.ID) {
		return &DeploymentConflict{Message: msgRegistrationDNSFollowBusy}
	}
	if changes && (HAEnabledModel(a) || HAActiveModel(a)) {
		return &DeploymentConflict{Message: OxidnsHALocked}
	}
	if input.profile != "" {
		profile = input.profile
	}
	if err := setRegistrationMembers(a, &input.record, a.ID, storedToken, profile, true); err != nil {
		return err
	}
	*result = RegistrationResult{ID: a.ID, Status: "online"}
	if owned {
		result.Token = new(storedToken)
	}
	return ctx.Err()
}

func setRegistrationMembers(a, input *model.Agent, id, token, profile string, refresh bool) error {
	original := *a
	*a = model.Agent{}
	for _, field := range []struct {
		name     string
		fallback any
		input    bool
	}{
		{"id", id, false}, {"name", nil, true}, {"host", nil, true}, {"port", nil, true},
		{"token", token, false}, {"service_type", nil, true}, {"profile_id", profile, false},
		{"deployment_method", "unknown", true}, {"status", "online", false}, {"last_heartbeat", nowISO(), false},
		{"version", nil, true}, {"config_version", "0", false}, {"enabled", true, false}, {"created_at", nowISO(), false},
	} {
		var raw json.RawMessage
		if refresh && (field.name == "config_version" || field.name == "created_at") {
			var err error
			raw, _, err = original.MemberJSON(field.name)
			if err != nil {
				return err
			}
		}
		if field.input {
			var present bool
			var err error
			raw, present, err = input.MemberJSON(field.name)
			if err != nil {
				return err
			}
			if !present && refresh && field.name == "deployment_method" {
				raw, _, err = original.MemberJSON(field.name)
				if err != nil {
					return err
				}
			}
		}
		if len(raw) == 0 {
			var err error
			raw, err = json.Marshal(field.fallback)
			if err != nil {
				return err
			}
		}
		if err := a.SetJSON(field.name, raw); err != nil {
			return err
		}
	}
	if !refresh {
		return nil
	}
	updated, err := json.Marshal(nowISO())
	if err != nil {
		return err
	}
	if err := a.SetJSON("updated_at", updated); err != nil {
		return err
	}
	// Rebuild the record from register_agent's fixed member list. Members
	// outside it (Docker HA enrollment marker, runtime metrics, unknown
	// extensions) are dropped as in Python: a retained enrollment marker would
	// let that installation credential replay this Agent's token after the
	// device itself registered. No nested history or R5 token is reconstructed
	// from its typed zero value.
	for _, field := range []struct{ name, fallback string }{
		{"deployments", `{}`}, {"latest_deployment", `null`}, {"latest_upgrade", `null`}, {"domain_discovery_enabled", `false`},
		{"traffic_history_enabled", ``}, {"traffic_history_since", ``}, {"config_revision", ``}, {"pushed_at", ``}, {"applied_content", ``},
		{"dns_follow", ``}, {"dns_follow_state", ``}, {"dns_follow_job", ``}, {"rule_sync", ``},
		{"oxidns_ha", ``}, {"oxidns_ha_state", ``}, {"oxidns_ha_publication", ``}, {"ipv6_verification_settings", ``}, {"ipv6_verifications", ``},
		{"gateway_installation_id", ``},
	} {
		raw, present, err := original.MemberJSON(field.name)
		if err != nil {
			return err
		}
		if !present {
			raw = json.RawMessage(field.fallback)
		}
		if field.name == "gateway_installation_id" && len(raw) > 0 {
			// Python keeps the trusted installation marker only when truthy.
			keep, err := model.JSONHasContent(raw)
			if err != nil {
				return err
			}
			if !keep {
				continue
			}
		}
		if len(raw) > 0 {
			if err := a.SetJSON(field.name, raw); err != nil {
				return err
			}
		}
	}
	return nil
}
