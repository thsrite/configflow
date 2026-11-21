package repo

import (
	"context"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// Section writes (N8-5b): one transaction that edits one section and keeps
// the section's bookkeeping, as Python's update_*_transaction did.
//
//   - UpdateProfile edits a profile's settings (the members of
//     ProfileFields); its identity and metadata stay. Removing a proxy group
//     that a rule still targets fails with ErrInUse. Rulesets keep only their
//     library reference (their source lives in the rule library). The
//     profile's revision advances by one and updated_at is set.
//   - UpdateShared edits the shared resources. Removing or disabling a
//     resource that is still referenced fails with ErrInUse. The shared
//     revision advances by one.
//
// Members a callback does not change keep their stored bytes; see Update.

// UpdateProfile runs fn on a copy of the profile's settings in one write
// transaction and returns the profile's config revision (profile revision
// plus shared revision) after the commit.
func (r *Repository) UpdateProfile(ctx context.Context, profileID string, fn func(ctx context.Context, p *model.Profile) error) (revision int64, err error) {
	ctx = ctxx.Detach(ctx)
	err = r.Update(ctx, func(ctx context.Context, d *model.Document) error {
		profile, err := updateProfileIn(d, profileID, func(p *model.Profile) error { return fn(ctx, p) })
		if err != nil {
			return err
		}
		revision = int64(profile.Revision) + int64(d.Shared.Revision)
		return nil
	})
	return revision, err
}

// UpdateShared runs fn on a copy of the shared resources in one write
// transaction.
func (r *Repository) UpdateShared(ctx context.Context, fn func(ctx context.Context, s *model.Shared) error) error {
	ctx = ctxx.Detach(ctx)
	return r.Update(ctx, func(ctx context.Context, d *model.Document) error {
		_, err := applySharedIn(d, func(s *model.Shared) error { return fn(ctx, s) })
		return err
	})
}

// UpdateSharedAndProfiles runs fn on copies of the shared resources and of
// every profile in one write transaction: profiles fn changed get the
// bookkeeping of UpdateProfile, and the shared resources that of
// UpdateShared, checked against the profiles as fn left them (so a resource
// can be removed together with its last reference). Any failure writes
// nothing.
func (r *Repository) UpdateSharedAndProfiles(ctx context.Context, fn func(ctx context.Context, s *model.Shared, profiles []*model.Profile) error) error {
	ctx = ctxx.Detach(ctx)
	return r.Update(ctx, func(ctx context.Context, d *model.Document) error {
		shared, err := copyRecord(&d.Shared)
		if err != nil {
			return err
		}
		profiles := make([]*model.Profile, len(d.Profiles))
		before := make([][]byte, len(d.Profiles))
		for index, stored := range d.Profiles {
			if profiles[index], err = copyRecord(stored); err != nil {
				return err
			}
			if before[index], err = model.Marshal(stored); err != nil {
				return err
			}
		}
		if err := fn(ctx, shared, profiles); err != nil {
			return err
		}
		for index, stored := range d.Profiles {
			after, err := model.Marshal(profiles[index])
			if err != nil {
				return err
			}
			if string(after) == string(before[index]) {
				continue
			}
			if _, err := updateProfileIn(d, stored.Key(), func(p *model.Profile) error {
				*p = *profiles[index]
				return nil
			}); err != nil {
				return err
			}
		}
		_, err = applySharedIn(d, func(s *model.Shared) error {
			*s = *shared
			return nil
		})
		return err
	})
}

// profileFor is the stored profile of a document by id (ErrValidation for a
// malformed id, ErrNotFound for an unknown one).
func profileFor(document *model.Document, profileID string) (*model.Profile, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	profile := profileByKey(document, profileID)
	if profile == nil {
		return nil, notFound(profileID)
	}
	return profile, nil
}

// updateProfileIn is _update_profile_locked on a typed document: fn edits a
// copy whose settings are stored back; see UpdateProfile.
func updateProfileIn(document *model.Document, profileID string, fn func(p *model.Profile) error) (*model.Profile, error) {
	current, err := profileFor(document, profileID)
	if err != nil {
		return nil, err
	}
	previousNames := groupNames(current)
	working, err := copyRecord(current)
	if err != nil {
		return nil, err
	}
	if err := fn(working); err != nil {
		return nil, err
	}
	// Only the settings are the callback's: identity, metadata and members
	// outside ProfileFields stay as stored.
	working.ID, working.Name, working.Description = current.ID, current.Name, current.Description
	working.CreatedAt, working.UpdatedAt, working.Revision = current.CreatedAt, current.UpdatedAt, current.Revision
	working.Rules, working.RuleSets = current.Rules, current.RuleSets
	*current = *working
	currentNames := groupNames(current)
	for _, rule := range current.RuleConfigs {
		if rule.Policy != nil && previousNames[*rule.Policy] && !currentNames[*rule.Policy] {
			return nil, inUse("配置 "+current.Name+" 的规则仍引用被删除策略组："+*rule.Policy,
				Usage{Kind: "profile", ID: profileID, Name: current.Name})
		}
	}
	for index := range current.RuleConfigs {
		rule := &current.RuleConfigs[index]
		if rule.ItemType != "ruleset" {
			continue
		}
		for _, member := range append(slices.Clone(RuleSourceFields), "library_enabled") {
			if err := rule.Clear(member); err != nil {
				return nil, err
			}
		}
	}
	current.Revision++
	current.UpdatedAt = nowISO()
	return current, nil
}

func groupNames(profile *model.Profile) map[string]bool {
	names := map[string]bool{}
	for _, group := range profile.ProxyGroups {
		names[group.Name] = true
	}
	return names
}

// sharedItem is the identity of one shared resource for the deletion guard.
type sharedItem struct {
	id, name string
	enabled  bool
}

func sharedItems(shared *model.Shared, kind string) ([]sharedItem, error) {
	var items []sharedItem
	add := func(record model.SwitchMember, id, name string, enabled *bool) {
		items = append(items, sharedItem{id, name, storedEnabled(record, enabled)})
	}
	switch kind {
	case kindSubscriptions:
		for index := range shared.Subscriptions {
			item := &shared.Subscriptions[index]
			add(item, item.ID, item.Name, item.Enabled)
		}
	case kindNodes:
		for index := range shared.Nodes {
			item := &shared.Nodes[index]
			add(item, item.ID, item.Name, item.Enabled)
		}
	case kindAggregations:
		for index := range shared.Aggregations {
			item := &shared.Aggregations[index]
			add(item, item.ID, item.Name, item.Enabled)
		}
	case kindRuleLibrary:
		for index := range shared.RuleLibrary {
			item := &shared.RuleLibrary[index]
			add(item, item.ID, item.Name, item.Enabled)
		}
	}
	return items, nil
}

// displayName is a resource's name, or its id when it has none.
func displayName(name, id string) string {
	if name == "" {
		return id
	}
	return name
}

// applySharedIn is _apply_shared_locked on a typed document: fn edits a copy
// of the shared resources, which replaces them after the deletion guard;
// see UpdateShared.
func applySharedIn(document *model.Document, fn func(s *model.Shared) error) (*model.Shared, error) {
	proposed, err := copyRecord(&document.Shared)
	if err != nil {
		return nil, err
	}
	if err := fn(proposed); err != nil {
		return nil, err
	}
	for _, kind := range SharedFields {
		proposedItems, err := sharedItems(proposed, kind)
		if err != nil {
			return nil, err
		}
		currentItems, err := sharedItems(&document.Shared, kind)
		if err != nil {
			return nil, err
		}
		remaining := map[string]sharedItem{}
		for _, item := range proposedItems {
			if item.id == "" {
				return nil, validationError(kind + " 中每一项必须有 ID")
			}
			if _, duplicate := remaining[item.id]; duplicate {
				return nil, validationError(kind + " 存在重复 ID：" + item.id)
			}
			remaining[item.id] = item
		}
		for _, item := range currentItems {
			kept, found := remaining[item.id]
			disabled := kind != kindRuleLibrary && item.enabled && !kept.enabled
			if found && !disabled {
				continue
			}
			usages, err := resourceUsages(document, kind, item.id)
			if err != nil {
				return nil, err
			}
			if len(usages) > 0 {
				names := make([]string, len(usages))
				rawUsages := make([]any, len(usages))
				for index, usage := range usages {
					names[index] = usage.Name
					rawUsages[index] = usage
				}
				return nil, inUse("资源 "+displayName(item.name, item.id)+" 仍被引用："+strings.Join(names, "、"), rawUsages...)
			}
		}
	}
	proposed.Revision = document.Shared.Revision + 1
	document.Shared = *proposed
	return &document.Shared, nil
}

// resourceUsage is _resource_usage: the aggregations, nodes and profiles
// that reference a shared resource, as usage records.
func resourceUsages(document *model.Document, kind, resourceID string) ([]Usage, error) {
	usages := []Usage{}
	shared := &document.Shared
	if kind == kindSubscriptions || kind == kindNodes {
		for _, aggregation := range shared.Aggregations {
			members := aggregation.Subscriptions
			if kind == kindNodes {
				members = aggregation.Nodes
			}
			if slices.Contains(members, resourceID) {
				usages = append(usages, Usage{Kind: "aggregation", ID: aggregation.ID, Name: displayName(aggregation.Name, aggregation.ID)})
			}
		}
	}
	if kind == kindSubscriptions {
		for _, node := range shared.Nodes {
			if node.SubscriptionID == resourceID {
				usages = append(usages, Usage{Kind: "node", ID: node.ID, Name: displayName(node.Name, node.ID)})
			}
		}
	}
	c := catalogsOf(shared)
	for _, profile := range document.Profiles {
		selected, err := selectResources(profile, c, nil)
		if err != nil {
			return nil, err
		}
		referenced := false
		if kind == kindRuleLibrary {
			for _, rule := range profile.RuleConfigs {
				referenced = referenced || rule.LibraryRuleID == resourceID
			}
		} else if set := selected[kind]; set != nil {
			referenced = set.has(resourceID)
		}
		if referenced {
			usages = append(usages, profileUsageRecord(profile))
		}
	}
	return usages, nil
}

func profileUsageRecord(profile *model.Profile) Usage {
	id, name := profile.ID, profile.Name
	return Usage{Kind: "profile", ID: id, Name: name, ProfileID: &id, ProfileName: &name}
}
