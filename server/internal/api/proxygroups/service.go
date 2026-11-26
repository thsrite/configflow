package proxygroups

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/substore"
)

// Service owns group writes and their transaction boundary.
type Service struct {
	Repo     *repo.Repository
	SubStore *substore.Service
}

// List reads the active profile's stored groups.
func (s *Service) List(ctx context.Context, id string) ([]model.ProxyGroup, error) {
	config, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if config.Profile.ProxyGroups == nil {
		return []model.ProxyGroup{}, nil
	}
	return config.Profile.ProxyGroups, nil
}

// Create validates a complete new group before adding it in one transaction.
func (s *Service) Create(ctx context.Context, id string, sent json.RawMessage, base string) (result, error) {
	ctx = lifecycle.Detach(ctx)
	out := result{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	req, err := decodeGroupRequest(sent)
	if err != nil {
		return out, err
	}
	if req.DeleteFields.Present() {
		return out, bad(msgDeleteCreate)
	}
	kind := req.Type.Value
	req.clean(kind)
	req.initHealthDefaults(kind)
	group, typeErr := req.buildGroup()
	err = s.edit(ctx, id, func(ctx context.Context, config *model.Config) error {
		before := profileBefore(config)
		groups := config.Profile.ProxyGroups
		if err := s.validate(ctx, group, req.groupMembers(), groups, nil, nil); err != nil {
			return err
		}
		if typeErr != nil {
			return groupTypeError(groups, len(groups), req, typeErr)
		}
		config.Profile.ProxyGroups = append(slices.Clone(groups), group)
		out.Warnings, err = s.syncFollow(ctx, before, config, base)
		return err
	})
	if err == nil {
		out.Data = &group
	}
	return out, err
}

// Mutate edits or deletes a group, including listeners, references and DNS.
func (s *Service) Mutate(ctx context.Context, id, groupID string, sent json.RawMessage, remove bool, base string) (result, error) {
	ctx = lifecycle.Detach(ctx)
	out := result{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var patch *groupRequest
	if !remove {
		var err error
		patch, err = decodeGroupRequest(sent)
		if err != nil {
			return out, err
		}
	}
	err := s.edit(ctx, id, func(ctx context.Context, config *model.Config) error {
		before := profileBefore(config)
		groups := slices.Clone(config.Profile.ProxyGroups)
		index := find(groups, groupID)
		if index < 0 {
			return absent("Group not found")
		}
		stored := groups[index]
		var next *groupRequest
		var group model.ProxyGroup
		var typeErr error
		if !remove {
			if patch.ID.Present() && (!patch.ID.OK || patch.ID.Value != groupID) {
				return conflict(msgImmutableID)
			}
			if err := shared.CheckFilterMembers(patch.groupMembers(), nil); err != nil {
				return err
			}
			deletes, err := shared.DeleteFields(patch.DeleteFields, patch.hasHealthField)
			if err != nil {
				return err
			}
			baseGroup, err := requestFromGroup(stored)
			if err != nil {
				return err
			}
			kind := patch.Type.Value
			if !patch.Type.Present() {
				kind = stored.Type
			}
			if !healthKind(stored.Type) && healthKind(kind) {
				baseGroup.initHealthDefaults(kind)
			}
			next = overlayGroup(baseGroup, patch, deletes)
			idRaw, _ := json.Marshal(groupID)
			next.ID = member[string]{Raw: idRaw, Value: groupID, OK: true}
			next.clean(kind)
			group, typeErr = next.buildGroup()
			proposed := slices.Clone(groups)
			proposed[index] = group
			beforeMembers := shared.GroupMembersOf(stored)
			if err = s.validate(ctx, group, next.groupMembers(), proposed, &stored, &beforeMembers); err != nil {
				return err
			}
			for i, other := range groups {
				if i != index && other.Name == next.Name.Value {
					return conflict(msgNameExists)
				}
			}
		}
		removing := remove || !next.enabled()
		if removing {
			used, err := referenced(config, groups, stored, groupID)
			if err != nil {
				return err
			}
			if used {
				return conflict(msgUsed)
			}
		}
		if removing || stored.Name != next.Name.Value {
			var name *string
			if !removing {
				name = new(next.Name.Value)
			}
			var err error
			out.ListenerChanges, err = relink(config, stored.Name, name)
			if err != nil {
				return err
			}
		}
		if remove {
			config.Profile.ProxyGroups = slices.Delete(groups, index, index+1)
		} else {
			proposed := slices.Clone(groups)
			proposed[index] = group
			for _, other := range groups {
				if model.TextValue(other.FollowGroup) == groupID {
					otherMembers := shared.GroupMembersOf(other)
					if err := s.validate(ctx, other, otherMembers, proposed, &other, &otherMembers); err != nil {
						return err
					}
				}
			}
			if stored.Name != next.Name.Value {
				rules := slices.Clone(config.Profile.RuleConfigs)
				for i := range rules {
					if model.TextValue(rules[i].Policy) == stored.Name {
						rules[i].Policy = new(next.Name.Value)
					}
				}
				config.Profile.RuleConfigs = rules
			}
			if typeErr != nil {
				return groupTypeError(groups, index, next, typeErr)
			}
			groups[index] = group
			config.Profile.ProxyGroups = groups
			out.Data = &group
		}
		var err error
		out.Warnings, err = s.syncFollow(ctx, before, config, base)
		if err != nil {
			return err
		}
		if len(out.ListenerChanges) > 0 {
			message := msgRename
			if removing {
				message = msgDetach
			}
			out.Message = fmt.Sprintf(message, len(out.ListenerChanges))
		}
		return nil
	})
	return out, err
}

// HealthURLRequest is the body of a batch probe URL change: one URL for the
// selected groups.
type HealthURLRequest struct {
	IDs member[[]string]        `json:"ids"`
	URL member[json.RawMessage] `json:"url"`
}

// BatchHealthURL updates only the selected groups' probe URL, atomically.
func (s *Service) BatchHealthURL(ctx context.Context, id string, body *HealthURLRequest) ([]model.ProxyGroup, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids := body.IDs.Value
	if !body.IDs.OK || len(ids) == 0 {
		return nil, bad(msgBatchIDs)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] {
			return nil, bad(msgBatchIDs)
		}
		seen[id] = true
	}
	health, err := healthURLObject(body.URL)
	if err != nil {
		return nil, err
	}
	if err = validateHealthURL(health); err != nil {
		return nil, err
	}
	var url *string
	if err := json.Unmarshal(body.URL.Raw, &url); body.URL.Present() && err != nil {
		return nil, err
	}
	out := []model.ProxyGroup{}
	err = s.edit(ctx, id, func(_ context.Context, config *model.Config) error {
		groups := slices.Clone(config.Profile.ProxyGroups)
		positions := make([]int, len(ids))
		for i, id := range ids {
			n := find(groups, id)
			if n < 0 {
				return absent(fmt.Sprintf(msgGroupMissing, id))
			}
			group := groups[n]
			if model.TextValue(group.FollowGroup) != "" {
				return bad(fmt.Sprintf(msgFollowHealth, group.Name))
			}
			if !healthKind(group.Type) {
				return bad(fmt.Sprintf(msgNoHealth, group.Name))
			}
			positions[i] = n
		}
		out = out[:0]
		for _, i := range positions {
			groups[i].URL = url
			out = append(out, groups[i])
		}
		config.Profile.ProxyGroups = groups
		return nil
	})
	return out, err
}

// Reorder moves stored groups by ID without accepting response display fields.
func (s *Service) Reorder(ctx context.Context, id string, body json.RawMessage) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	order := []string{}
	err := s.edit(ctx, id, func(_ context.Context, config *model.Config) error {
		groups, missing := reorder(config.Profile.ProxyGroups, body)
		if len(missing) > 0 {
			return absent(fmt.Sprintf(msgOrder, missingText(missing)))
		}
		config.Profile.ProxyGroups = groups
		if err := validateDialers(config); err != nil {
			return err
		}
		order = order[:0]
		for _, group := range groups {
			order = append(order, group.ID)
		}
		return nil
	})
	return order, err
}

// reorder applies a reorder request; a malformed request is reported as
// the only missing ID, as before.
func reorder(groups []model.ProxyGroup, body json.RawMessage) ([]model.ProxyGroup, []string) {
	if body == nil {
		body = json.RawMessage("{}")
	}
	request, err := shared.DecodeOrder(body, "groups")
	if err != nil {
		return groups, []string{err.Error()}
	}
	return shared.Reorder(groups, request.IDs, request.Position, func(group model.ProxyGroup) string { return group.ID })
}

func referenced(config *model.Config, groups []model.ProxyGroup, original model.ProxyGroup, id string) (bool, error) {
	for _, row := range config.Profile.RuleConfigs {
		if model.Flag(row.Enabled, true) && model.TextValue(row.Policy) == original.Name {
			return true, nil
		}
	}
	for _, group := range groups {
		if group.ID == id {
			continue
		}
		if group.IncludeGroups != nil && slices.Contains(*group.IncludeGroups, id) || model.TextValue(group.FollowGroup) == id {
			return true, nil
		}
	}
	return incoming(config, id)
}
