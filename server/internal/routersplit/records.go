package routersplit

import (
	"configflow/internal/gateway/gwbase"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"sync"
)

// HookMissingError means the setup-session provider was not registered.
type HookMissingError struct{ Hook, Provider string }

func (e *HookMissingError) Error() string {
	return "common.router_split: " + e.Hook + " is not available (provided by " + e.Provider + ")"
}

// SessionReader reads setup-owned session records before a profile transaction.
type SessionReader func(context.Context, *repo.Repository, string) ([]Session, error)

var sessionReaderState struct {
	sync.RWMutex
	read SessionReader
}

// SetSessionReader installs the typed setup owner and returns a restoration.
func SetSessionReader(read SessionReader) func() {
	sessionReaderState.Lock()
	old := sessionReaderState.read
	sessionReaderState.read = read
	sessionReaderState.Unlock()
	return func() { sessionReaderState.Lock(); sessionReaderState.read = old; sessionReaderState.Unlock() }
}
func readSessions(ctx context.Context, r *repo.Repository, id string) (*Context, error) {
	sessionReaderState.RLock()
	read := sessionReaderState.read
	sessionReaderState.RUnlock()
	if read == nil {
		return nil, &HookMissingError{"setup_flow.profile_sessions", "config C36 (common/setup_flow.py)"}
	}
	rows, err := read(ctx, r, id)
	if err != nil {
		return nil, err
	}
	return SessionContext(rows)
}

// ReadContext captures setup evidence and strict Agent binding identities.
func ReadContext(ctx context.Context, r *repo.Repository, id string) (*Context, error) {
	bindings, err := r.ProfileAgentBindingsRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	out, err := readSessions(ctx, r, id)
	if err != nil {
		return nil, err
	}
	out.MihomoAgentIDs = []string{}
	for _, a := range bindings.Agents {
		var service string
		_ = json.Unmarshal(a.ServiceType, &service)
		if service != "mihomo" {
			continue
		}
		var identity *string
		if json.Unmarshal(a.ID, &identity) != nil || identity == nil || *identity == "" {
			return nil, identityError(messageBindingsInvalid)
		}
		out.MihomoAgentIDs = append(out.MihomoAgentIDs, *identity)
	}
	return out, nil
}

// FullTunNode applies the native takeover to an owned generated YAML document.
func FullTunNode(document *yamlx.Node) (*yamlx.Node, error) {
	root := yamlx.Resolve(document)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, gatewayError(messageDocumentObject)
	}
	return fullTunNode(document)
}

// TunFilterPaths lists active exclusions that prevent full-TUN takeover.
func TunFilterPaths(tun *yamlx.Node) []string { return nodeTunFilters(tun) }

// TunActive reports the same native enabled evidence as the takeover guard.
func TunActive(node *yamlx.Node) bool { return nodeFilterActive(node) }

// FullTunShape validates the generated native takeover document.
func FullTunShape(document *yamlx.Node) bool {
	return fullTunNodeShape(document) && len(nodeTunFilters(yamlx.Get(document, "tun"))) == 0
}

// ProfileRuleIDs returns an owned list of valid takeover rule identities.
func ProfileRuleIDs(profile *model.Profile) []string {
	state, marker := ReadMarkerProfile(profile)
	if state != StateValid {
		return []string{}
	}
	return append([]string{}, marker.RouteRuleSetIDs...)
}

// PublishSource reads session evidence for an unmarked publishing profile.
func PublishSource(ctx context.Context, r *repo.Repository, id string, profile *model.Profile, agentID string) (string, error) {
	state, _ := ReadMarkerProfile(profile)
	if state != StateAbsent || id == "" {
		return "", nil
	}
	c, err := readSessions(ctx, r, id)
	if err != nil {
		return "", err
	}
	if agentID == "" {
		return "", identityError(messageTopologyIdentity)
	}
	c.MihomoAgentIDs = []string{agentID}
	return LegacyProfileSource(profile, c)
}

// WasGatewayUsed recognizes setup purpose and the retained installation proof.
func WasGatewayUsed(profile *model.Profile, agent *model.Agent, source string) (bool, error) {
	if source == "setup" || (profile != nil && profile.Mihomo.Fields.Has(gwbase.RestoreKey)) {
		return true, nil
	}
	if agent == nil {
		return false, nil
	}
	return agent.GatewayInstallationID != nil && *agent.GatewayInstallationID != "", nil
}
