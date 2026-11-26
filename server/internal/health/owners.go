package health

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

func init() { currentProbeEnv.Collaborators = defaultCollaborators(nil) }

func defaultCollaborators(converterFn func() convert.NodeConverter) Collaborators {
	return Collaborators{
		NeedsRemoteConversion: needsRemoteProxyString,
		ConvertNode: func(ctx context.Context, raw json.RawMessage) (model.Fields, bool) {
			var converter convert.NodeConverter
			if converterFn != nil {
				converter = converterFn()
			}
			return convertProbeNode(ctx, raw, converter)
		},
		LoadSubscriptionCache: func(ctx context.Context, r *repo.Repository, id string) (json.RawMessage, error) {
			cache, err := substore.LoadCache(ctx, r, id)
			if err != nil || cache == nil {
				return nil, err
			}
			return json.Marshal(cache)
		},
		FilterSubscriptionNodes: func(sub model.Subscription, nodes []json.RawMessage) ([]json.RawMessage, error) {
			typed := make([]model.Node, 0, len(nodes))
			for _, raw := range nodes {
				var node model.Node
				if json.Unmarshal(raw, &node) == nil {
					typed = append(typed, node)
				}
			}
			kept := subs.FilterNodes(sub, typed)
			out := make([]json.RawMessage, 0, len(kept))
			for _, node := range kept {
				raw, err := json.Marshal(node)
				if err != nil {
					return nil, err
				}
				out = append(out, raw)
			}
			return out, nil
		},
		ReadLatencyFile: func(ctx context.Context, r *repo.Repository) (json.RawMessage, bool) {
			if r == nil {
				return nil, false
			}
			data, err := r.ReadSharedBytes(ctx, LatencyFile)
			if err != nil || !json.Valid(data) {
				return nil, false
			}
			return json.RawMessage(data), true
		},
		WriteLatencyFile: func(ctx context.Context, r *repo.Repository, data json.RawMessage) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if r == nil {
				return &ValidationError{Message: msgSaveLatencyFailed}
			}
			if !json.Valid(data) {
				return &ValidationError{Message: msgSaveLatencyFailed}
			}
			_, err := r.WriteSharedText(ctx, LatencyFile, append(data, '\n'))
			return err
		},
	}
}

// ConvertNodeForProbe materializes one stored probe candidate with its injected
// typed URI converter, preserving already materialized protocol extensions.
func ConvertNodeForProbe(ctx context.Context, raw json.RawMessage, converter convert.NodeConverter) (model.Fields, bool) {
	return convertProbeNode(ctx, raw, converter)
}
