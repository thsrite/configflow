package gateway

import (
	"fmt"
	"regexp"

	"configflow/internal/agents"
)

// Agent version gates used by publication and the gateway controller.
const (
	MinAgentVersion                     = "1.8.25-go"
	ForwardingMinAgentVersion           = "1.8.40-go"
	StandaloneForwardingMinAgentVersion = "1.8.41-go"
)

var agentVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-go)?$`)

// SupportsGateway reports whether the Agent implements the gateway protocol.
func SupportsGateway(version string) bool { return agentAtLeast(version, MinAgentVersion) }

// SupportsForwarding reports whether gateway removal preserves persisted forwarding.
func SupportsForwarding(version string) bool { return agentAtLeast(version, ForwardingMinAgentVersion) }

// SupportsStandaloneForwarding reports standalone forwarding support.
func SupportsStandaloneForwarding(version string) bool {
	return agentAtLeast(version, StandaloneForwardingMinAgentVersion)
}
func agentAtLeast(version, minimum string) bool {
	if !agentVersionPattern.MatchString(version) {
		return false
	}
	return agents.CompareVersions(version, minimum) >= 0
}

// RequireGatewayAgent refuses an Agent that cannot apply the generated gateway settings.
func RequireGatewayAgent(version string) error {
	if !SupportsGateway(version) {
		return &Error{Message: fmt.Sprintf(msgGatewayUpgrade, MinAgentVersion)}
	}
	return nil
}
