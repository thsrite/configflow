package agents

// Legacy registry/repository/rules boundaries are isolated here. Client tools
// use typed requests and delegate validation to shared config owners.
import (
	"context"
	"encoding/json"

	registry "configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/ctxx"
	"configflow/internal/deploy"
	"configflow/internal/discovery"
	"configflow/internal/faultprobe"
	"configflow/internal/health"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

type clientDevice struct {
	ID          string
	Name        string
	ProfileID   string
	ServiceType string
	record      *registry.Record
}

func clientAgent(ctx context.Context, id string, history bool) (*clientDevice, error) {
	manager, err := registry.Get(ctx)
	if err != nil {
		return nil, err
	}
	record, err := manager.GetAgentByID(ctx, id, history)
	if err != nil || record == nil {
		return nil, err
	}
	return &clientDevice{record.GetString("id"), record.GetString("name"), record.GetString("profile_id"), record.GetString("service_type"), record}, nil
}
func clientBody(c *httpx.Ctx) map[string]json.RawMessage {
	v := c.JSONSilent()
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	return fields
}
func clientLabel(ctx context.Context, id string) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	p, err := r.GetProfile(ctx, id)
	if err != nil {
		return "", err
	}
	name := p.GetString("name")
	if name == "" {
		name = id
	}
	return name, nil
}
func clientConfig(ctx context.Context, id string, fresh bool) (*jsonx.Object, error) {
	if !fresh {
		return repo.GetConfig(ctx, id)
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.GetCompatConfig(ctx, id, repo.ResourceRoots{})
}
func freshClientDiscovery(ctx context.Context, id string) (discovery.Settings, error) {
	config, err := clientConfig(ctx, id, true)
	if err != nil {
		return discovery.Settings{}, err
	}
	return clientDiscoverySettings(config)
}
func clientDiscoverySettings(config *jsonx.Object) (discovery.Settings, error) {
	var payload struct {
		Settings discovery.Settings `json:"domain_discovery"`
	}
	raw, err := json.Marshal(config)
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	return payload.Settings, err
}
func clientNotesPayload(ctx context.Context, id string) (any, error) {
	config, err := clientConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	notes, err := oxidns.ReadNotes(oxidns.NewProfile(config))
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
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		if err := checkClientBinding(tx, id, edit.ProfileID, false); err != nil {
			return nil, err
		}
		p := oxidns.NewProfile(config)
		notes, err := oxidns.ReadNotes(p)
		if err != nil {
			return nil, clientValidation(err.Error())
		}
		if notes[edit.IP] != edit.Previous {
			return nil, repo.NewInUse(msgClientNoteConflict, nil)
		}
		if err := oxidns.SetNote(p, edit.IP, edit.Name); err != nil {
			return nil, clientValidation(err.Error())
		}
		return nil, nil
	}, edit.ProfileID)
	return err
}
func clientRuleDeps() (rules.ConnectionDeps, *error) {
	var failure error
	return rules.ConnectionDeps{
		RulesetInfo: func(snapshot *jsonx.Object, id string) *jsonx.Object {
			raw, err := json.Marshal(snapshot)
			if err != nil {
				failure = err
				return nil
			}
			info, err := discovery.QueryRuleset(raw, id)
			if err != nil {
				failure = err
				return nil
			}
			raw, err = json.Marshal(info)
			if err != nil {
				failure = err
				return nil
			}
			v, err := jsonx.Parse(raw)
			if err != nil {
				failure = err
				return nil
			}
			o, _ := v.(*jsonx.Object)
			return o
		},
		ReadRulePayload:   rulefetch.ReadRulePayloadAny,
		AppendRulePayload: rulefetch.AppendRulePayloadAny,
	}, &failure
}
func clientTargetsPayload(ctx context.Context, id string) (any, error) {
	config, err := clientConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	deps, failure := clientRuleDeps()
	targets := rules.IPRuleTargets(config, deps)
	if *failure != nil {
		return nil, *failure
	}
	if targets == nil {
		targets = []*jsonx.Object{}
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
		IPRulesets  []*jsonx.Object        `json:"ip_rulesets"`
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
	var result *rules.IPRuleResult
	_, err = repo.UpdateSharedConfigTransaction(ctx, func(tx context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := checkClientBinding(tx, id, edit.ProfileID, true); err != nil {
			return nil, err
		}
		config, err := clientConfig(tx, edit.ProfileID, true)
		if err != nil {
			return nil, err
		}
		deps, failure := clientRuleDeps()
		result, err = rules.ApplyIPRule(deps, config, shared, edit.RulesetID, edit.Target, dest, edit.RuleType)
		if *failure != nil {
			return nil, *failure
		}
		if err != nil {
			return nil, err
		}
		for _, item := range result.Updated {
			raw, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			var declaration model.RuleLibraryItem
			if err = json.Unmarshal(raw, &declaration); err != nil {
				return nil, err
			}
			stage, err = rulefetch.StageCache(tx, declaration, []byte(item.GetString("content")), nil)
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
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
		Success  bool  `json:"success"`
		Added    []any `json:"added"`
		Skipped  []any `json:"skipped"`
		Warnings []any `json:"warnings"`
	}{true, result.Added, result.Skipped, result.Warnings}, nil
}
func updateClientPolicies(ctx context.Context, id string, edit policyEdit) error {
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		latest, err := clientAgent(tx, id, true)
		if err != nil {
			return nil, err
		}
		profile := ""
		if latest != nil {
			profile, err = effectiveClientProfile(tx, latest.ProfileID)
			if err != nil {
				return nil, err
			}
		}
		if latest == nil || latest.ServiceType != "oxidns" || profile != edit.ProfileID {
			return nil, repo.NewInUse(msgPolicyConflict, nil)
		}
		p := oxidns.NewProfile(config)
		settings := p.Settings()
		if settings == nil {
			settings = oxidns.M()
		}
		if edit.Revision != oxidns.Revision(settings) {
			return nil, repo.NewInUse(msgPolicyConflict, nil)
		}
		notesRevision, err := oxidns.PolicyNamesRevision(p)
		if err != nil {
			return nil, err
		}
		if edit.NotesRevision != notesRevision {
			return nil, repo.NewInUse(msgPolicyConflict, nil)
		}
		rows := oxidns.Null()
		if len(edit.Policies) > 0 {
			rows, err = oxidns.FromJSON(edit.Policies)
			if err != nil {
				return nil, err
			}
		}
		saved, err := oxidns.SavePolicyRows(p, settings, rows, edit.BaseURL)
		if err != nil {
			return nil, err
		}
		config.Set("oxidns", oxidns.ToJSONX(saved))
		return nil, nil
	}, edit.ProfileID)
	return err
}
func clientPolicyPayload(ctx context.Context, id string, agent *clientDevice) (any, error) {
	config, err := clientConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	p := oxidns.NewProfile(config)
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
	all, err := manager.GetAllAgents(ctx, false)
	if err != nil {
		return nil, err
	}
	type peer struct {
		ID   string          `json:"id"`
		Name json.RawMessage `json:"name"`
	}
	peers := []peer{}
	for _, record := range all {
		if record.GetString("service_type") != "oxidns" {
			continue
		}
		bound, err := effectiveClientProfile(ctx, record.GetString("profile_id"))
		if err != nil {
			return nil, err
		}
		if bound == id {
			label := record.Value("name")
			if !jsonx.Truthy(label) {
				label = record.Value("id")
			}
			raw, err := json.Marshal(label)
			if err != nil {
				return nil, err
			}
			peers = append(peers, peer{record.GetString("id"), raw})
		}
	}
	var deployment *registry.Record
	if record := agent.record.GetObject("latest_deployment"); record != nil && record.Len() > 0 {
		deployment = deploy.PublicDeployment(record)
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
		Deployment    *registry.Record   `json:"deployment"`
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
	traffic, err := trafficstore.Get(ctx)
	if err != nil {
		return false, err
	}
	if err = traffic.Delete(id); err != nil {
		return false, err
	}
	probes, err := health.GetProbeStore(ctx)
	if err != nil {
		return false, err
	}
	if err = probes.Delete(id); err != nil {
		return false, err
	}
	regions, err := health.GetRegionStore(ctx)
	if err != nil {
		return false, err
	}
	if err = regions.Delete(id); err != nil {
		return false, err
	}
	return true, nil
}
