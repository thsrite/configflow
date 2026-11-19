package agents

import (
	"context"
	"encoding/json"
	"strings"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// ProfileIdentity is the metadata returned by a reviewed profile binding move.
type ProfileIdentity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AgentBindingIdentity contains only device identity, never credentials or
// deployment payloads. Raw members preserve R5 presentation token types.
type AgentBindingIdentity struct {
	ID          string          `json:"id"`
	Name        json.RawMessage `json:"name"`
	Host        json.RawMessage `json:"host"`
	Port        json.RawMessage `json:"port"`
	ServiceType json.RawMessage `json:"service_type"`
}

// ProfileRebinding describes the exact reviewed set moved by RebindProfiles.
type ProfileRebinding struct {
	FromProfile ProfileIdentity        `json:"from_profile"`
	ToProfile   ProfileIdentity        `json:"to_profile"`
	Agents      []AgentBindingIdentity `json:"agents"`
}

func (m *Manager) profileIdentity(ctx context.Context, id string) (ProfileIdentity, error) {
	var result ProfileIdentity
	if err := repo.ValidateProfileID(id); err != nil {
		return result, err
	}
	err := m.repository.View(ctx, func(document *model.Document) error {
		for _, p := range document.Profiles {
			if p.Key() == id {
				result = ProfileIdentity{p.ID, p.Name, p.Description, p.CreatedAt, p.UpdatedAt}
				return nil
			}
		}
		return &repo.Error{Kind: repo.ErrNotFound, Message: id}
	})
	return result, err
}

// RebindProfiles atomically moves exactly the reviewed original Agent IDs. A
// deployment/preparation started before the lock is seen by the same guards.
func (m *Manager) RebindProfiles(ctx context.Context, source, target string, expectedIDs []string) (*ProfileRebinding, error) {
	if err := repo.ValidateProfileID(source); err != nil {
		return nil, err
	}
	if err := repo.ValidateProfileID(target); err != nil {
		return nil, err
	}
	if source == target {
		return nil, repo.NewValidationError(msgRebindOtherProfile)
	}
	expected := make(map[string]bool, len(expectedIDs))
	for _, id := range expectedIDs {
		if id == "" || expected[id] {
			return nil, repo.NewValidationError(msgRebindReviewedIDs)
		}
		expected[id] = true
	}
	if len(expected) == 0 {
		return nil, repo.NewValidationError(msgRebindReviewedIDs)
	}
	var result ProfileRebinding
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		var err error
		result.FromProfile, err = m.profileIdentity(tx, source)
		if err != nil {
			return err
		}
		result.ToProfile, err = m.profileIdentity(tx, target)
		if err != nil {
			return err
		}
		defaultID, err := m.defaultProfileID(tx)
		if err != nil {
			return err
		}
		bound := make([]*model.Agent, 0, len(expected))
		ids := make(map[string]bool)
		for i := range *records {
			a := &(*records)[i]
			raw, present, err := a.MemberJSON("profile_id")
			if err != nil {
				return err
			}
			profile, valid := storedText(raw, present, nil)
			if !valid && !storedAbsent(raw, present, nil) {
				continue
			}
			if profile == "" {
				profile = defaultID
			}
			if profile != source {
				continue
			}
			if !agentIDProven(a) {
				return repo.NewInUse(msgRebindChanged, nil)
			}
			bound = append(bound, a)
			ids[a.ID] = true
		}
		if len(ids) != len(expected) {
			return repo.NewInUse(msgRebindChanged, nil)
		}
		for id := range ids {
			if !expected[id] {
				return repo.NewInUse(msgRebindChanged, nil)
			}
		}
		var blocked []string
		for _, a := range bound {
			if upgradeBusyModel(a) || HAActiveModel(a) || unfinishedDeploymentModel(a) {
				name := a.Name
				if name == "" {
					name = a.ID
				}
				blocked = append(blocked, registryDiagnostic(a, name, DetailLimit))
			}
		}
		if len(blocked) > 0 {
			return &DeploymentConflict{Message: msgRebindBusy + strings.Join(blocked, "、")}
		}
		result.Agents = make([]AgentBindingIdentity, 0, len(bound))
		for _, a := range bound {
			view := AgentBindingIdentity{ID: a.ID}
			for _, field := range []struct {
				name, fallback string
				destination    *json.RawMessage
			}{
				{"name", `null`, &view.Name}, {"host", `""`, &view.Host}, {"port", `null`, &view.Port}, {"service_type", `"mihomo"`, &view.ServiceType},
			} {
				raw, present, err := a.MemberJSON(field.name)
				if err != nil {
					return err
				}
				if !present {
					raw = json.RawMessage(field.fallback)
				}
				if field.name == "name" && (storedAbsent(raw, true, nil) || a.Name == "") {
					raw, err = json.Marshal(a.ID)
					if err != nil {
						return err
					}
				}
				if field.name == "service_type" && (storedAbsent(raw, true, nil) || a.ServiceType == "") {
					raw = json.RawMessage(`"mihomo"`)
				}
				*field.destination = raw
			}
			result.Agents = append(result.Agents, view)
			a.ProfileID, a.UpdatedAt = target, new(nowISO())
		}
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
