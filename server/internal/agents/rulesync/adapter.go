package rulesync

// This file alone adapts legacy repository/Agent records. Reconciliation and
// projection use typed native JSON, immutable deploy.Source and yamlx nodes.
import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
)

func decodeAgent(record *agents.Record) (*model.Agent, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode agent snapshot: %w", err)
	}
	var a model.Agent
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	return &a, nil
}

// SettingsFor reads strictly boolean scopes; historical wrong types are off.
func SettingsFor(record *agents.Record) Settings {
	a, err := decodeAgent(record)
	if err != nil || a.RuleSync == nil {
		return Settings{}
	}
	s := a.RuleSync.Settings
	return Settings{Library: s.Library != nil && *s.Library, Rules: s.Rules != nil && *s.Rules}
}

func publishSource(ctx context.Context, m *agents.Manager, profileID string) (*deploy.Source, error) {
	raw, revision, err := m.Repository().GetPublishSnapshot(ctx, profileID, false)
	if err != nil {
		return nil, err
	}
	return deploy.NewSource(raw, &revision), nil
}

func persistSync(ctx context.Context, m *agents.Manager, id string, settings *Settings, state *model.RuleSyncState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := m.UpdateAgents(ctx, "", func(tx context.Context, list *[]any) (any, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		for _, entry := range *list {
			record, ok := entry.(*jsonx.Object)
			if !ok || record.GetString("id") != id {
				continue
			}
			sync := record.GetObject("rule_sync")
			if sync == nil {
				sync = jsonx.NewObject()
			}
			if settings != nil {
				sync.Set("settings", jsonx.Obj("library", settings.Library, "rules", settings.Rules))
			}
			if state != nil {
				raw, err := json.Marshal(state)
				if err != nil {
					return nil, err
				}
				value, err := jsonx.Parse(raw)
				if err != nil {
					return nil, err
				}
				sync.Set("state", value)
			}
			record.Set("rule_sync", sync)
			return nil, tx.Err()
		}
		return nil, ErrAgentNotFound
	})
	return err
}

func renderCandidate(ctx context.Context, target deploy.Target, source *deploy.Source, baseURL string) (string, []convert.Download, error) {
	raw, err := source.Raw()
	if err != nil {
		return "", nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", nil, err
	}
	config, ok := value.(*jsonx.Object)
	if !ok {
		return "", nil, refuse(msgSnapshotVerify)
	}
	if target.ServiceType == "mihomo" {
		content, err := convert.GenerateMihomoConfig(ctx, config, convert.GenerateOptions{BaseURL: baseURL, SyncLANHosts: true, SkipProviderPreflight: true, ForAgent: true, RegionProbe: target.DomainDiscovery})
		return content, nil, err
	}
	p := oxidns.NewProfile(config)
	content, err := oxidns.Generate(p, baseURL)
	if err != nil {
		return "", nil, err
	}
	entries, err := oxidns.RulesetDownloads(p, baseURL)
	if err != nil {
		return "", nil, err
	}
	downloads := make([]convert.Download, 0, len(entries))
	for _, e := range entries {
		downloads = append(downloads, convert.Download{URL: e.URL, LocalPath: e.LocalPath})
	}
	return content, downloads, nil
}
