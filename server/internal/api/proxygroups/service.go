package proxygroups

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/lifecycle"
)

// Service owns group writes and their transaction boundary.
type Service struct{}

// List reads the active profile's stored groups.
func (s *Service) List(ctx context.Context, id string) ([]object, error) {
	profile, err := load(ctx, id)
	if err != nil {
		return nil, err
	}
	return list(profile, "proxy_groups")
}
func clean(group *object) {
	kind := text(*group, "type")
	if !healthKind(kind) {
		for _, key := range healthFields() {
			group.Delete(key)
		}
	}
	if kind != "url-test" {
		group.Delete("tolerance")
	}
	if kind != "load-balance" {
		group.Delete("strategy")
	}
	if kind == "chain" {
		for _, key := range []string{"subscriptions", "regex", "exclude_regex", "manual_nodes", "aggregations", "aggregation_regex", "aggregation_exclude_regex", "include_groups", "follow_group", "proxies", "source", "source_type", "proxies_order", "url", "interval", "strategy", "lazy", "use", "include_all", "include-all"} {
			group.Delete(key)
		}
	} else {
		group.Delete("chain")
	}
}

// Create validates a complete new group before adding it in one transaction.
func (s *Service) Create(ctx context.Context, id string, group object, base string) (result, error) {
	ctx = lifecycle.Detach(ctx)
	out := result{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if group.Has("delete_fields") {
		return out, bad(msgDeleteCreate)
	}
	clean(&group)
	var err error
	group, err = initializeHealth(group)
	if err != nil {
		return out, err
	}
	err = edit(ctx, id, func(ctx context.Context, profile *object) error {
		before := *profile
		if err := validate(ctx, group, *profile, nil); err != nil {
			return err
		}
		groups, err := list(*profile, "proxy_groups")
		if err != nil {
			return err
		}
		if err = set(profile, "proxy_groups", append(groups, group)); err != nil {
			return err
		}
		out.Warnings, err = syncFollow(ctx, before, profile, base)
		return err
	})
	out.Data = &group
	return out, err
}

// Mutate edits or deletes a group, including listeners, references and DNS.
func (s *Service) Mutate(ctx context.Context, id, groupID string, patch object, remove bool, base string) (result, error) {
	ctx = lifecycle.Detach(ctx)
	out := result{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	err := edit(ctx, id, func(ctx context.Context, profile *object) error {
		before := *profile
		groups, err := list(*profile, "proxy_groups")
		if err != nil {
			return err
		}
		index := find(groups, groupID)
		if index < 0 {
			return absent("Group not found")
		}
		original := groups[index]
		var next object
		if !remove {
			if raw, ok := patch.Get("id"); ok {
				var ident string
				if json.Unmarshal(raw, &ident) != nil || ident != groupID {
					return conflict(msgImmutableID)
				}
			}
			baseGroup := original
			kind := text(patch, "type")
			if !patch.Has("type") {
				kind = text(original, "type")
			}
			if !healthKind(text(original, "type")) && healthKind(kind) {
				if err = set(&baseGroup, "type", kind); err != nil {
					return err
				}
				baseGroup, err = initializeHealth(baseGroup)
				if err != nil {
					return err
				}
			}
			next, err = merge(baseGroup, patch)
			if err != nil {
				return err
			}
			if err = set(&next, "id", groupID); err != nil {
				return err
			}
			clean(&next)
			final := *profile
			proposed := slices.Clone(groups)
			proposed[index] = next
			if err = set(&final, "proxy_groups", proposed); err != nil {
				return err
			}
			if err = validate(ctx, next, final, &original); err != nil {
				return err
			}
			for i, group := range groups {
				if i != index && text(group, "name") == text(next, "name") {
					return conflict(msgNameExists)
				}
			}
		}
		removing := remove || !enabled(next)
		if removing {
			used, err := referenced(*profile, groups, original, groupID)
			if err != nil {
				return err
			}
			if used {
				return conflict(msgUsed)
			}
		}
		if removing || text(original, "name") != text(next, "name") {
			var name *string
			if !removing {
				value := text(next, "name")
				name = &value
			}
			out.ListenerChanges, err = relink(profile, text(original, "name"), name)
			if err != nil {
				return err
			}
		}
		if remove {
			groups = append(groups[:index:index], groups[index+1:]...)
		} else {
			final := *profile
			proposed := slices.Clone(groups)
			proposed[index] = next
			if err = set(&final, "proxy_groups", proposed); err != nil {
				return err
			}
			for _, group := range groups {
				if text(group, "follow_group") == groupID {
					copy := group
					if err = validate(ctx, group, final, &copy); err != nil {
						return err
					}
				}
			}
			if text(original, "name") != text(next, "name") {
				rules, err := list(*profile, "rule_configs")
				if err != nil {
					return err
				}
				for i := range rules {
					if text(rules[i], "policy") == text(original, "name") {
						if err = set(&rules[i], "policy", text(next, "name")); err != nil {
							return err
						}
					}
				}
				if err = set(profile, "rule_configs", rules); err != nil {
					return err
				}
			}
			groups[index] = next
			out.Data = &next
		}
		if err = set(profile, "proxy_groups", groups); err != nil {
			return err
		}
		out.Warnings, err = syncFollow(ctx, before, profile, base)
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

// BatchHealthURL updates only the selected groups' probe URL, atomically.
func (s *Service) BatchHealthURL(ctx context.Context, id string, body object) ([]object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids, err := value[[]string](body, "ids")
	if err != nil || len(ids) == 0 {
		return nil, bad(msgBatchIDs)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] {
			return nil, bad(msgBatchIDs)
		}
		seen[id] = true
	}
	if err = validateHealthURL(body); err != nil {
		return nil, err
	}
	out := []object{}
	err = edit(ctx, id, func(_ context.Context, profile *object) error {
		groups, err := list(*profile, "proxy_groups")
		if err != nil {
			return err
		}
		positions := make([]int, len(ids))
		for i, id := range ids {
			n := find(groups, id)
			if n < 0 {
				return absent(fmt.Sprintf(msgGroupMissing, id))
			}
			group := groups[n]
			if text(group, "follow_group") != "" {
				return bad(fmt.Sprintf(msgFollowHealth, fallbackName(group)))
			}
			if !healthKind(text(group, "type")) {
				return bad(fmt.Sprintf(msgNoHealth, fallbackName(group)))
			}
			positions[i] = n
		}
		for _, i := range positions {
			raw, _ := body.Get("url")
			if err = groups[i].Set("url", raw); err != nil {
				return err
			}
			out = append(out, groups[i])
		}
		return set(profile, "proxy_groups", groups)
	})
	return out, err
}

// Reorder moves stored groups by ID without accepting response display fields.
func (s *Service) Reorder(ctx context.Context, id string, body object) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	order := []string{}
	err := edit(ctx, id, func(_ context.Context, profile *object) error {
		groups, err := list(*profile, "proxy_groups")
		if err != nil {
			return err
		}
		groups, missing, err := reorder(groups, body)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return absent(fmt.Sprintf(msgOrder, missingText(missing)))
		}
		if err = set(profile, "proxy_groups", groups); err != nil {
			return err
		}
		if err = validateDialers(*profile); err != nil {
			return err
		}
		for _, group := range groups {
			order = append(order, text(group, "id"))
		}
		return nil
	})
	return order, err
}

func contains(values []string, want string) bool { return slices.Contains(values, want) }
func referenced(profile object, groups []object, original object, id string) (bool, error) {
	rows, err := list(profile, "rule_configs")
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if enabled(row) && text(row, "policy") == text(original, "name") {
			return true, nil
		}
	}
	for _, group := range groups {
		if text(group, "id") == id {
			continue
		}
		ids, err := value[[]string](group, "include_groups")
		if err != nil && group.Has("include_groups") {
			return false, err
		}
		if contains(ids, id) || text(group, "follow_group") == id {
			return true, nil
		}
	}
	return incoming(profile, id)
}
