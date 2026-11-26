package routersplit

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
)

func readSessions(ctx context.Context, r *repo.Repository, id string) (*Context, error) {
	sessions, err := setupflow.ProfileSessions(ctx, r.DataDir(), id)
	if err != nil {
		return nil, err
	}
	rows := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		var value Session
		if err := json.Unmarshal(session.Raw, &value); err != nil {
			return nil, err
		}
		rows = append(rows, value)
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
	if source == "setup" || (profile != nil && !profile.Mihomo.LinuxGatewayRestorePoint.IsZero()) {
		return true, nil
	}
	if agent == nil {
		return false, nil
	}
	return agent.GatewayInstallationID != nil && *agent.GatewayInstallationID != "", nil
}
