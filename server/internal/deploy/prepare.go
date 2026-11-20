package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/provider"
	"context"
	"encoding/json"
	"errors"
)

// PrepareOptions controls preparation cache writes and progress reporting.
type PrepareOptions struct {
	BaseURL, DeploymentID        string
	PreferCached, StoreDownloads bool
	Progress                     Progress
}

// Prepared is the frozen delivery and cache updates validated with its provider graph.
type Prepared struct {
	Bundle                                 *Bundle
	ConfigContent                          string
	Providers                              []*provider.Artifact
	SubscriptionHosts, SubscriptionDialers json.RawMessage
	ProviderDownloads                      []ProviderFile
	RulesetDownloads                       []convert.ResourceDownload
	CustomFiles                            []CustomFile
}

// AgentConfig is the legacy pull response's generated content and digest.
type AgentConfig struct {
	Content     string `json:"content"`
	MD5         string `json:"md5"`
	Version     string `json:"version"`
	ServiceType string `json:"service_type"`
}

// PrepareContent generates and validates the entire candidate before any upload.
func PrepareContent(ctx context.Context, m *agents.Manager, t Target, src *Source, o PrepareOptions) (p *Prepared, err error) {
	err = RunDetached(ctx, "agent-prepare-"+t.ID, func(ctx context.Context) error {
		var e error
		p, e = prepareContent(ctx, m, t, src, o)
		return e
	})
	return
}

// PrepareContentBundle is the read-only content comparison preparation entry.
func PrepareContentBundle(ctx context.Context, m *agents.Manager, t Target, src *Source, baseURL, id string) (*Bundle, error) {
	p, err := PrepareContent(ctx, m, t, src, PrepareOptions{BaseURL: baseURL, DeploymentID: id, PreferCached: true})
	if err != nil {
		return nil, err
	}
	return p.Bundle, nil
}

// GenerateAgentConfig generates the legacy pull payload without creating a deployment.
func GenerateAgentConfig(ctx context.Context, src *Source, t Target, baseURL string) (AgentConfig, error) {
	return generateAgentConfig(ctx, src, t, baseURL)
}

// Refusal is a safety guard refusal with a user-facing message.
type Refusal struct{ Message string }

func (e *Refusal) Error() string { return e.Message }

// IsPreparationFailure identifies the errors whose public preparation response is 400.
func IsPreparationFailure(err error) bool {
	var p *PreparationError
	var r *Refusal
	if errors.As(err, &p) || errors.As(err, &r) {
		return true
	}
	return preparationFailure(err)
}
