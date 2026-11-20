package configgen

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Candidate holds a materialized download and the exact profile generation used
// to create it. Source remains private to the frozen artifact owner.
type Candidate struct {
	Content             string
	Archive             []byte
	Source              json.RawMessage
	Revision            int64
	SubscriptionHosts   json.RawMessage
	SubscriptionDialers json.RawMessage
}

// Capture pins the resolved profile and revision before generation. Render and
// archive creation reuse Build's implementation; generated paths are never read
// back, since another request may replace them immediately.
func Capture(ctx context.Context, profileID string, engine Engine, baseURL string) (Candidate, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	id, err := repo.ResolveProfileID(ctx, profileID)
	if err != nil {
		return Candidate{}, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return Candidate{}, err
	}
	config, revision, err := r.PublishConfig(ctx, id, false)
	if err != nil {
		return Candidate{}, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return Candidate{}, err
	}
	download, err := build(ctx, config, engine, baseURL)
	if err != nil {
		return Candidate{}, err
	}
	hosts := download.Output.SubscriptionHosts
	if len(hosts) == 0 {
		hosts = json.RawMessage("null")
	}
	dialers := download.Output.SubscriptionDialers
	if len(dialers) == 0 {
		dialers = json.RawMessage("null")
	}
	return Candidate{Content: download.Output.Content, Archive: download.Data, Source: raw, Revision: revision, SubscriptionHosts: hosts, SubscriptionDialers: dialers}, nil
}
