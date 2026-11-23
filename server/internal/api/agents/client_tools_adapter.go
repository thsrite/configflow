package agents

// Client tools use typed registry/config models and shared rule validators.
import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"time"

	registry "configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/ctxx"
	"configflow/internal/discovery"
	"configflow/internal/faultprobe"
	"configflow/internal/health"
	"configflow/internal/httpx"

	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

type clientDevice struct {
	ID          string
	Name        string
	ProfileID   string
	ServiceType string
	record      *model.Agent
}

func clientAgent(ctx context.Context, id string, history bool) (*clientDevice, error) {
	manager, err := registry.Get(ctx)
	if err != nil {
		return nil, err
	}
	record, err := manager.ReadAgent(ctx, id, history)
	if err != nil || record == nil {
		return nil, err
	}
	profile, err := manager.ResolveAgentProfile(ctx, record)
	if err != nil {
		if errors.Is(err, repo.ErrValidation) {
			return nil, &clientBindingError{err}
		}
		return nil, err
	}
	return &clientDevice{record.ID, record.Name, profile, record.ServiceType, record}, nil
}
func clientBody(c *httpx.Ctx) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if !c.BindSilent(&fields) {
		return nil
	}
	return fields
}
func clientLabel(ctx context.Context, id string) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	p, err := r.Profile(ctx, id)
	if err != nil {
		return "", err
	}
	name := p.Name
	if name == "" {
		name = id
	}
	return name, nil
}
func clientConfig(ctx context.Context, id string, fresh bool) (*model.Config, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	var roots repo.ResourceRoots
	if fresh {
		roots = repo.ResourceRoots{}
	}
	return r.Config(ctx, id, roots)
}
func clientPolicyConfig(ctx context.Context, id string, fresh bool) (*model.Config, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	var roots repo.ResourceRoots
	if fresh {
		roots = repo.ResourceRoots{}
	}
	return r.ConfigWithOxiDNSAgents(ctx, id, roots)
}
func freshClientDiscovery(ctx context.Context, id string) (discovery.Settings, error) {
	config, err := clientConfig(ctx, id, true)
	if err != nil {
		return discovery.Settings{}, err
	}
	return clientDiscoverySettings(config)
}
func clientDiscoverySettings(config *model.Config) (discovery.Settings, error) {
	var settings discovery.Settings
	raw, err := model.Marshal(config.Profile.DomainDiscovery)
	if err != nil {
		return settings, err
	}
	err = jsondecode.Unmarshal(raw, &settings)
	return settings, err
}
func clientNotesPayload(ctx context.Context, id string) (any, error) {
	config, err := clientConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	p, err := oxidns.NewTypedProfile(config)
	if err != nil {
		return nil, err
	}
	notes, err := oxidns.ReadNotes(p)
	if err != nil {
		return nil, err
	}
	name, err := clientLabel(ctx, id)
	return struct {
		Success     bool              `json:"success"`
		ProfileID   string            `json:"profile_id"`
		ProfileName string            `json:"profile_name"`
		Notes       map[string]string `json:"notes"`
	}{true, id, name, notes}, err
}
func updateClientNote(ctx context.Context, id string, edit noteEdit) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateProfile(ctx, edit.ProfileID, func(tx context.Context, profile *model.Profile) error {
		if err := checkClientBinding(tx, id, edit.ProfileID, false); err != nil {
			return err
		}
		p, err := oxidns.NewTypedProfile(&model.Config{Profile: profile, ProfileID: edit.ProfileID})
		if err != nil {
			return err
		}
		notes, err := oxidns.ReadNotes(p)
		if err != nil {
			return clientValidation(err.Error())
		}
		if notes[edit.IP] != edit.Previous {
			return repo.NewInUse(msgClientNoteConflict, nil)
		}
		if err := oxidns.SetNote(p, edit.IP, edit.Name); err != nil {
			return clientValidation(err.Error())
		}
		return profile.SetJSON("device_notes", p.DeviceNotesJSON())
	})
	return err
}
func clientRuleDeps(config *model.Config) (rules.IPDeps, error) {
	raw, err := model.Marshal(config)
	if err != nil {
		return rules.IPDeps{}, err
	}
	return rules.IPDeps{
		Describe: func(id string) (rules.RulesetInfo, error) {
			info, err := discovery.QueryRuleset(raw, id)
			if err != nil {
				return rules.RulesetInfo{}, err
			}
			result := rules.RulesetInfo{ID: info.ID}
			if info.Name != nil {
				result.Name = *info.Name
			}
			if info.Behavior != nil {
				result.Behavior = *info.Behavior
			}
			for _, ref := range info.References {
				result.References = append(result.References, rules.RuleReference{Enabled: ref.Enabled, AfterMatch: ref.AfterMatch, Policy: ref.Policy})
			}
			return result, nil
		},
		ReadPayload: rulefetch.ReadRulePayload, AppendPayload: rulefetch.AppendRulePayload,
	}, nil
}
func clientTargetsPayload(ctx context.Context, id string) (any, error) {
	config, err := clientConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	deps, err := clientRuleDeps(config)
	if err != nil {
		return nil, err
	}
	targets, err := rules.IPTargets(config.RuleLibrary, deps)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		targets = []rules.IPTarget{}
	}
	settings, err := clientDiscoverySettings(config)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var direct, proxy *discovery.RulesetInfo
	if settings.DirectRuleset != nil {
		direct, err = discovery.QueryRuleset(raw, *settings.DirectRuleset)
		if err != nil {
			return nil, err
		}
	}
	if settings.ProxyRuleset != nil {
		proxy, err = discovery.QueryRuleset(raw, *settings.ProxyRuleset)
		if err != nil {
			return nil, err
		}
	}
	name, err := clientLabel(ctx, id)
	return struct {
		Success     bool                   `json:"success"`
		ProfileID   string                 `json:"profile_id"`
		ProfileName string                 `json:"profile_name"`
		IPRulesets  []rules.IPTarget       `json:"ip_rulesets"`
		Direct      *discovery.RulesetInfo `json:"direct"`
		Proxy       *discovery.RulesetInfo `json:"proxy"`
	}{true, id, name, targets, direct, proxy}, err
}
func applyClientIP(ctx context.Context, id string, edit connectionEdit, dest *rules.Destination) (any, error) {
	ctx, release, err := rulefetch.LockCacheEdits(ctx, edit.RulesetID)
	if err != nil {
		return nil, err
	}
	defer release()
	var stage *rulefetch.CachePublication
	defer func() { stage.Discard(ctx) }()
	var result *rules.IPResult
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	err = r.UpdateShared(ctx, func(tx context.Context, shared *model.Shared) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := checkClientBinding(tx, id, edit.ProfileID, true); err != nil {
			return err
		}
		config, err := clientConfig(tx, edit.ProfileID, true)
		if err != nil {
			return err
		}
		deps, err := clientRuleDeps(config)
		if err != nil {
			return err
		}
		result, err = rules.AppendIP(deps, shared.RuleLibrary, edit.RulesetID, edit.Target, dest, edit.RuleType)
		if err != nil {
			return err
		}
		for _, item := range result.Updated {
			var content string
			if item.Content != nil {
				content = *item.Content
			}
			stage, err = rulefetch.StageCache(tx, item, []byte(content), nil)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stage != nil {
		faultprobe.Point(ctx, faultprobe.RuleDBCommitted)
		if err = stage.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return struct {
		Success  bool              `json:"success"`
		Added    []rules.IPAdded   `json:"added"`
		Skipped  []rules.IPSkipped `json:"skipped"`
		Warnings []string          `json:"warnings"`
	}{true, result.Added, result.Skipped, result.Warnings}, nil
}
func updateClientPolicies(ctx context.Context, id string, edit policyEdit) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateProfile(ctx, edit.ProfileID, func(tx context.Context, stored *model.Profile) error {
		latest, err := clientAgent(tx, id, true)
		if err != nil {
			return err
		}
		profile := ""
		if latest != nil {
			profile = latest.ProfileID
		}
		if latest == nil || latest.ServiceType != "oxidns" || profile != edit.ProfileID {
			return repo.NewInUse(msgPolicyConflict, nil)
		}
		config, err := clientPolicyConfig(tx, edit.ProfileID, true)
		if err != nil {
			return err
		}
		p, err := oxidns.NewTypedProfile(config)
		if err != nil {
			return err
		}
		settings := p.Settings()
		if settings == nil {
			settings = oxidns.M()
		}
		if edit.Revision != oxidns.Revision(settings) {
			return repo.NewInUse(msgPolicyConflict, nil)
		}
		notesRevision, err := oxidns.PolicyNamesRevision(p)
		if err != nil {
			return err
		}
		if edit.NotesRevision != notesRevision {
			return repo.NewInUse(msgPolicyConflict, nil)
		}
		rows := oxidns.Null()
		if len(edit.Policies) > 0 {
			rows, err = oxidns.FromJSON(edit.Policies)
			if err != nil {
				return err
			}
		}
		saved, err := oxidns.SavePolicyRows(p, settings, rows, edit.BaseURL)
		if err != nil {
			return err
		}
		if err := stored.SetJSON("oxidns", oxidns.ToJSON(saved)); err != nil {
			return err
		}
		return stored.SetJSON("device_notes", p.DeviceNotesJSON())
	})
	return err
}
func clientPolicyPayload(ctx context.Context, id string, agent *clientDevice) (any, error) {
	config, err := clientPolicyConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	p, err := oxidns.NewTypedProfile(config)
	if err != nil {
		return nil, err
	}
	settings := p.Settings()
	if settings == nil {
		settings = oxidns.M()
	}
	view, err := oxidns.ReadPolicies(settings, p)
	if err != nil {
		return nil, err
	}
	notes, err := oxidns.ReadNotes(p)
	if err != nil {
		return nil, err
	}
	notesRevision, err := oxidns.PolicyNamesRevision(p)
	if err != nil {
		return nil, err
	}
	name, err := clientLabel(ctx, id)
	if err != nil {
		return nil, err
	}
	manager, err := registry.Get(ctx)
	if err != nil {
		return nil, err
	}
	all, err := manager.ReadAgents(ctx, false, nil)
	if err != nil {
		return nil, err
	}
	type peer struct {
		ID   string          `json:"id"`
		Name json.RawMessage `json:"name"`
	}
	peers := []peer{}
	for _, record := range all {
		if record.ServiceType != "oxidns" {
			continue
		}
		bound, err := manager.ResolveAgentProfile(ctx, &record)
		if err != nil {
			if errors.Is(err, repo.ErrValidation) {
				warnClientPeer(&record, time.Now())
				continue
			}
			return nil, err
		}
		if bound == id {
			raw := rawAgentMember(&record, "name")
			hasContent, err := model.Truthy(raw)
			if err != nil {
				return nil, err
			}
			if !hasContent {
				raw = rawAgentMember(&record, "id")
			}
			peers = append(peers, peer{record.ID, raw})
		}
	}
	var deployment json.RawMessage
	if agent.record.LatestDeployment != nil {
		deployment, err = publicDeploymentModel(agent.record.LatestDeployment)
		if err != nil {
			return nil, err
		}
		original, err := model.Marshal(agent.record.LatestDeployment)
		if err != nil {
			return nil, err
		}
		deployment = redact.SanitizeCredentials(deployment, redact.CollectCredentials(json.RawMessage(original), nil))
	}
	return struct {
		Success       bool               `json:"success"`
		Draft         bool               `json:"draft"`
		ProfileID     string             `json:"profile_id"`
		ProfileName   string             `json:"profile_name"`
		Revision      string             `json:"revision"`
		NotesRevision string             `json:"notes_revision"`
		Agents        []peer             `json:"agents"`
		Notes         map[string]string  `json:"device_notes"`
		Deployment    json.RawMessage    `json:"deployment"`
		Policies      json.RawMessage    `json:"policies"`
		Editable      bool               `json:"editable"`
		Reason        string             `json:"reason"`
		SideEntries   []oxidns.SideEntry `json:"side_entries"`
	}{true, true, id, name, oxidns.Revision(settings), notesRevision, peers, notes, deployment, oxidns.ToJSON(view.Policies), view.Editable, view.Reason, view.SideEntries}, nil
}
func clearClientDiscovery(ctx context.Context, id string) (bool, error) {
	agent, err := clientAgent(ctx, id, false)
	if err != nil || agent == nil {
		return false, err
	}
	traffic, err := trafficstore.GetNative(ctx)
	if err != nil {
		return false, err
	}
	if err = traffic.DeleteContext(ctx, id); err != nil {
		return false, err
	}
	probes, err := health.GetProbeStore(ctx)
	if err != nil {
		return false, err
	}
	if err = probes.DeleteContext(ctx, id); err != nil {
		return false, err
	}
	regions, err := health.GetRegionStore(ctx)
	if err != nil {
		return false, err
	}
	if err = regions.DeleteContext(ctx, id); err != nil {
		return false, err
	}
	return true, nil
}
