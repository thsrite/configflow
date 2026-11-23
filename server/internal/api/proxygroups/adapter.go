package proxygroups

// Groups are edited on the resolved profile record; membership, health,
// listeners and DNS routing are delegated to their owners.
import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"configflow/internal/convert"
	"configflow/internal/ctxx"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

// requestObject returns the sent JSON object, or nil (ok false) for any other body.
func requestObject(c *httpx.Ctx, silent bool) (json.RawMessage, bool, error) {
	var raw json.RawMessage
	if silent {
		if !c.BindSilent(&raw) {
			return nil, false, nil
		}
	} else if err := c.Bind(&raw); err != nil {
		return nil, false, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false, nil
	}
	return raw, true, nil
}

// readGroup reads a group request as sent (see groupFields).
func readGroup(c *httpx.Ctx) (groupFields, bool, error) {
	raw, ok, err := requestObject(c, false)
	if err != nil || !ok {
		return groupFields{}, ok, err
	}
	var out groupFields
	err = out.UnmarshalJSON(raw)
	return out, true, err
}

// decodeObject decodes an object body into a request DTO.
func decodeObject[T any](c *httpx.Ctx, silent bool) (*T, bool, error) {
	raw, ok, err := requestObject(c, silent)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := new(T)
	err = jsondecode.Unmarshal(raw, out)
	return out, true, err
}

func load(ctx context.Context, id string) (*model.Config, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.Config(ctx, id, nil)
}
func edit(ctx context.Context, id string, fn func(context.Context, *model.Config) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return repo.UpdateConfig(ctx, id, func(tx context.Context, config *model.Config) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		if err := fn(tx, config); err != nil {
			return err
		}
		return tx.Err()
	})
}

// profileBefore is the profile as it is before an edit. Group edits replace the
// profile's members (new slices, new settings) instead of writing into them,
// so a copy of the profile record is enough.
func profileBefore(config *model.Config) *model.Config {
	copy := *config
	profile := *config.Profile
	copy.Profile = &profile
	return &copy
}

// fieldsOf is a stored group as sent members.
func fieldsOf(group model.ProxyGroup) (groupFields, error) {
	var out groupFields
	data, err := model.Marshal(group)
	if err == nil {
		err = out.UnmarshalJSON(data)
	}
	return out, err
}

// groupOf reads a group as the stored record does (a member of another
// type is dropped; the write reports it).
func groupOf(fields groupFields) (model.ProxyGroup, error) {
	var group model.ProxyGroup
	data, err := fields.MarshalJSON()
	if err == nil {
		err = json.Unmarshal(data, &group)
	}
	return group, err
}

// storeGroups stores groups with groups[index] replaced by (or, at
// len(groups), extended with) a group as sent. The profile record decodes
// the sent members strictly, so a member of another type is refused with the
// repository's message for the resulting list.
func storeGroups(config *model.Config, groups []model.ProxyGroup, index int, edited groupFields) error {
	list := make([]json.RawMessage, 0, len(groups)+1)
	for i, group := range groups {
		data, err := model.Marshal(group)
		if i == index {
			data, err = edited.MarshalJSON()
		}
		if err != nil {
			return err
		}
		list = append(list, data)
	}
	if index == len(groups) {
		data, err := edited.MarshalJSON()
		if err != nil {
			return err
		}
		list = append(list, data)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return config.Profile.SetJSON("proxy_groups", raw)
}

func healthKind(kind string) bool { return check.IsGroupHealthType(kind) }
func healthFields() []string      { return shared.HealthFields }
func initializeHealth(group groupFields) (groupFields, error) {
	err := check.InitGroupHealth(&group.Fields)
	return group, err
}

// validate checks a group as sent against the catalogs and the proposed
// profile groups; original is the stored group of an update.
func validate(ctx context.Context, group groupFields, groups []model.ProxyGroup, original *groupFields) error {
	g, err := groupOf(group)
	if err != nil {
		return err
	}
	var old *model.ProxyGroup
	var before *shared.GroupMembers
	if original != nil {
		v, err := groupOf(*original)
		if err != nil {
			return err
		}
		old = &v
		members, err := original.members()
		if err != nil {
			return err
		}
		before = &members
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	catalog, err := r.Shared(ctx)
	if err != nil {
		return err
	}
	// The request's own members are checked: the group record cannot hold a
	// member of the wrong type (O18 T1c).
	sent, err := group.members()
	if err != nil {
		return err
	}
	return shared.CheckCandidateMembers(g, sent, shared.GroupCatalog{Nodes: catalog.Nodes, Subscriptions: catalog.Subscriptions, Aggregations: catalog.Aggregations, Groups: groups}, old, before)
}

// merge applies a group patch to the stored group; the result keeps the
// request's values as sent, so validation sees a member of the wrong type
// before the group record decodes it.
func merge(original, patch groupFields) (groupFields, error) {
	group, err := groupOf(original)
	if err != nil {
		return groupFields{}, err
	}
	if _, err := shared.MergeGroup(group, patch.Fields); err != nil {
		return groupFields{}, err
	}
	var request struct {
		DeleteFields member[[]string] `json:"delete_fields"`
	}
	data, err := patch.MarshalJSON()
	if err != nil {
		return groupFields{}, err
	}
	_ = jsondecode.Unmarshal(data, &request)
	merged := original
	for field, value := range patch.All() {
		if field != "delete_fields" {
			if err := merged.Set(field, value); err != nil {
				return groupFields{}, err
			}
		}
	}
	for _, field := range request.DeleteFields.Value {
		merged.Delete(field)
	}
	return merged, nil
}
func incoming(config *model.Config, id string) (bool, error) {
	refs, err := convert.IncomingRefs(config, "group", id)
	return len(refs) > 0, err
}
func relink(config *model.Config, old string, next *string) ([]string, error) {
	return subs.RelinkListeners(config.Profile, old, next)
}

// healthURLObject is the health-check object of a batch URL change.
func healthURLObject(sent member[json.RawMessage]) (json.RawMessage, error) {
	raw := sent.Raw
	if raw == nil {
		raw = json.RawMessage("null")
	}
	return json.Marshal(struct {
		URL json.RawMessage `json:"url"`
	}{raw})
}
func validateHealthURL(health json.RawMessage) error { return check.ValidateHealthJSON(health, false) }
func missingText(ids []string) string                { return shared.IdentifierList(ids) }
func validateDialers(config *model.Config) error {
	return convert.DialerFailure(convert.ConfigDialers(config))
}

// syncFollow re-routes follow-DNS rules whose policy route changed; before
// is a copy of the profile taken before the edit.
func syncFollow(ctx context.Context, before, after *model.Config, base string) ([]string, error) {
	settings, err := after.Profile.OxiDNS.Node()
	if err != nil {
		return nil, err
	}
	following, ok := oxidns.SettingsStrings(settings, "rule_dns_follow")
	if !ok || len(following) == 0 {
		return nil, nil
	}
	catalog, err := sharedCatalog(ctx)
	if err != nil {
		return nil, err
	}
	view := func(config *model.Config) (*oxidns.Profile, error) {
		copy := *config
		copy.Nodes, copy.Subscriptions, copy.Aggregations, copy.RuleLibrary = catalog.Nodes, catalog.Subscriptions, catalog.Aggregations, catalog.RuleLibrary
		return oxidns.NewTypedProfile(&copy)
	}
	a, err := view(before)
	if err != nil {
		return nil, err
	}
	b, err := view(after)
	if err != nil {
		return nil, err
	}
	native := oxidns.SettingsText(settings, "config_mode") == "native"
	changed := false
	for _, row := range after.Profile.RuleConfigs {
		if !slices.Contains(following, row.ID) {
			continue
		}
		if native && (!model.Flag(row.Enabled, true) || !model.Flag(row.LibraryEnabled, true)) {
			continue
		}
		i := slices.IndexFunc(before.Profile.RuleConfigs, func(item model.RuleConfig) bool { return item.ID == row.ID })
		if i < 0 {
			continue
		}
		if oxidns.RuleFollowRoute(before.Profile.RuleConfigs[i], a) != oxidns.RuleFollowRoute(row, b) {
			changed = true
			break
		}
	}
	if !changed {
		return nil, nil
	}
	result, err := oxidns.ApplyRuleDNS(a, b, nil, oxidns.RuleDNSApplyOptions{BaseURL: base})
	if err != nil {
		return nil, err
	}
	if result.Changed {
		if err = after.Profile.OxiDNS.SetNode(result.Settings); err != nil {
			return nil, fmt.Errorf("store followed DNS routes: %w", err)
		}
	}
	return result.Warnings, nil
}

// healthDefaults are the probe defaults the editors start from.
type healthDefaults struct {
	Group    check.GroupHealth    `json:"group"`
	Provider check.ProviderHealth `json:"provider"`
}

func defaults() (healthDefaults, error) {
	g := check.DefaultGroupHealth()
	tolerance := int64(50)
	g.Tolerance = &tolerance
	return healthDefaults{Group: g, Provider: check.DefaultProviderHealth()}, nil
}

func sharedCatalog(ctx context.Context) (*model.Shared, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.Shared(ctx)
}

func cachedNodes(ctx context.Context, id string) ([]model.Node, error) {
	cache, err := substore.LoadCache(ctx, id)
	if err != nil || cache == nil {
		return nil, err
	}
	return cache.Nodes, nil
}

func boundNonempty(c *httpx.Ctx) bool {
	var raw json.RawMessage
	return c.BindSilent(&raw) && shared.EnabledField(raw, false)
}
