package proxygroups

// Legacy collaborators are confined to this boundary. The service retains
// native JSON members and delegates validation and rendering to their owners.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/ctxx"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

func native(v any) (object, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return object{}, err
	}
	var out object
	err = json.Unmarshal(raw, &out)
	return out, err
}
func configView(o object) (*model.Config, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	return model.DecodeConfig(raw)
}
func groupRecord(o object) (model.ProxyGroup, error) {
	var group model.ProxyGroup
	raw, err := json.Marshal(o)
	if err == nil {
		err = json.Unmarshal(raw, &group)
	}
	return group, err
}
func readBody(c *httpx.Ctx, silent bool) (object, bool, error) {
	var raw json.RawMessage
	if silent {
		if !c.BindSilent(&raw) {
			return object{}, false, nil
		}
	} else {
		if err := c.Bind(&raw); err != nil {
			return object{}, false, err
		}
	}
	if len(raw) == 0 || raw[0] != '{' {
		return object{}, false, nil
	}
	var out object
	err := json.Unmarshal(raw, &out)
	return out, true, err
}

func readReorderBody(c *httpx.Ctx) (object, error) {
	body, ok, err := readBody(c, false)
	if err != nil || ok {
		return body, err
	}
	if boundNonempty(c) {
		return object{}, &fault{500, "reorder request must be a JSON object"}
	}
	return object{}, nil
}
func load(ctx context.Context, id string) (object, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return object{}, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return object{}, err
	}
	value, err := r.Config(ctx, id, nil)
	if err != nil {
		return object{}, err
	}
	return native(value)
}
func edit(ctx context.Context, id string, fn func(context.Context, *object) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		p, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(tx, &p); err != nil {
			return nil, err
		}
		next, err := legacy(p)
		if err != nil {
			return nil, err
		}
		if err := tx.Err(); err != nil {
			return nil, err
		}
		raw.Update(next)
		return nil, nil
	}, id)
	return err
}
func healthKind(kind string) bool { return check.IsGroupHealthType(kind) }
func healthFields() []string      { return shared.HealthFields }
func initializeHealth(group object) (object, error) {
	err := check.InitGroupHealth(&group)
	return group, err
}
func validate(ctx context.Context, group, profile object, original *object) error {
	g, err := groupRecord(group)
	if err != nil {
		return err
	}
	p, err := configView(profile)
	if err != nil {
		return err
	}
	var old *model.ProxyGroup
	if original != nil {
		v, err := groupRecord(*original)
		if err != nil {
			return err
		}
		old = &v
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	catalog, err := r.Shared(ctx)
	if err != nil {
		return err
	}
	return shared.CheckCandidates(g, shared.GroupCatalog{Nodes: catalog.Nodes, Subscriptions: catalog.Subscriptions, Aggregations: catalog.Aggregations, Groups: p.Profile.ProxyGroups}, old)
}
func merge(original, patch object) (object, error) {
	group, err := groupRecord(original)
	if err != nil {
		return object{}, err
	}
	result, err := shared.MergeGroup(group, patch)
	if err != nil {
		return object{}, err
	}
	return native(result)
}
func enabled(o object) bool { return flag(o, "enabled", true) }
func incoming(profile object, id string) (bool, error) {
	p, err := configView(profile)
	if err != nil {
		return false, err
	}
	refs, err := convert.IncomingRefs(p, "group", id)
	return len(refs) > 0, err
}
func relink(profile *object, old string, next *string) ([]string, error) {
	p, err := configView(*profile)
	if err != nil {
		return nil, err
	}
	changes, err := subs.RelinkListeners(p.Profile, old, next)
	if err != nil {
		return nil, err
	}
	*profile, err = native(p)
	return changes, err
}
func validateHealthURL(body object) error {
	raw, _ := body.Get("url")
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	fields := object{}
	if err := fields.Set("url", raw); err != nil {
		return err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return check.ValidateHealthJSON(encoded, false)
}
func reorder(groups []object, body object) ([]object, []string, error) {
	request := map[string]json.RawMessage{}
	for key, raw := range body.All() {
		request[key] = raw
	}
	ids, err := shared.OrderIDs(request, "groups")
	if err != nil {
		return groups, []string{err.Error()}, nil
	}
	returnOrder, missing := shared.Reorder(groups, ids, orderPosition(body), func(group object) string { return text(group, "id") })
	return returnOrder, missing, nil
}
func missingText(ids []string) string { return shared.IdentifierList(ids) }
func validateDialers(profile object) error {
	p, err := configView(profile)
	if err != nil {
		return err
	}
	return convert.DialerFailure(convert.ConfigDialers(p))
}
func profileView(p object) (*oxidns.Profile, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(raw)
}
func ruleView(r object) (*oxidns.Node, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return oxidns.FromJSON(raw)
}
func syncFollow(ctx context.Context, before object, after *object, base string) ([]string, error) {
	settings := objectOf(*after, "oxidns")
	following, err := value[[]string](settings, "rule_dns_follow")
	if err != nil || len(following) == 0 {
		return nil, nil
	}
	previous, err := list(before, "rule_configs")
	if err != nil {
		return nil, err
	}
	current, err := list(*after, "rule_configs")
	if err != nil {
		return nil, err
	}
	shared, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	old, currentProfile := before, *after
	for key, raw := range shared.All() {
		if err = old.Set(key, raw); err != nil {
			return nil, err
		}
		if err = currentProfile.Set(key, raw); err != nil {
			return nil, err
		}
	}
	a, err := profileView(old)
	if err != nil {
		return nil, err
	}
	b, err := profileView(currentProfile)
	if err != nil {
		return nil, err
	}
	changed := false
	for _, row := range current {
		if !contains(following, text(row, "id")) {
			continue
		}
		if text(settings, "config_mode") == "native" && (!enabled(row) || !flag(row, "library_enabled", true)) {
			continue
		}
		i := find(previous, text(row, "id"))
		if i < 0 {
			continue
		}
		was, err := ruleView(previous[i])
		if err != nil {
			return nil, err
		}
		now, err := ruleView(row)
		if err != nil {
			return nil, err
		}
		if oxidns.FollowRoute(was, a) != oxidns.FollowRoute(now, b) {
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
		if err = after.Set("oxidns", oxidns.ToJSON(result.Settings)); err != nil {
			return nil, fmt.Errorf("store followed DNS routes: %w", err)
		}
	}
	return result.Warnings, nil
}
func flag(o object, key string, def bool) bool {
	raw, ok := o.Get(key)
	if !ok {
		return def
	}
	return shared.EnabledField(raw, def)
}

func defaults() (struct {
	Group    object `json:"group"`
	Provider object `json:"provider"`
}, error) {
	out := struct {
		Group    object `json:"group"`
		Provider object `json:"provider"`
	}{}
	g := check.DefaultGroupHealth()
	tolerance := int64(50)
	g.Tolerance = &tolerance
	var err error
	out.Group, err = native(g)
	if err != nil {
		return out, err
	}
	out.Provider, err = native(check.DefaultProviderHealth())
	return out, err
}

func sharedConfig(ctx context.Context) (object, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return object{}, err
	}
	value, err := r.Shared(ctx)
	if err != nil {
		return object{}, err
	}
	return native(value)
}

func names(o object, key string) ([]string, error) {
	raw := raw(o, key)
	if !shared.EnabledField(raw, false) {
		return nil, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil, bad(msgPreviewSelection)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		var id *string
		if json.Unmarshal(entry, &id) == nil && id != nil {
			ids = append(ids, *id)
		}
	}
	return ids, nil
}
func filterSubscription(sub object, nodes []object) ([]object, error) {
	raw, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}
	var source model.Subscription
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	// Filtering is name-based; retain the original node records without copying parameters.
	matcher := subs.CompileFilter(source)
	out := make([]object, 0, len(nodes))
	for _, node := range nodes {
		if matcher == nil || matcher.Search(text(node, "name")) {
			out = append(out, node)
		}
	}
	return out, nil
}
func filterAggregation(nodes []object, pattern string) ([]object, error) {
	empty := ""
	return shared.Filter(nodes, &pattern, &empty, func(node object) string { return text(node, "name") })
}

func cachedNodes(ctx context.Context, id string) ([]object, error) {
	cache, err := substore.LoadCache(ctx, id)
	if err != nil {
		return nil, err
	}
	if cache == nil {
		return []object{}, nil
	}
	out := make([]object, len(cache.Nodes))
	for i, node := range cache.Nodes {
		out[i], err = native(node)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func readPreviewBody(c *httpx.Ctx) (object, bool, error) {
	body, ok, err := readBody(c, false)
	if err != nil {
		return object{}, false, &fault{500, "invalid JSON request"}
	}
	if !ok && !boundNonempty(c) {
		return object{}, true, nil
	}
	return body, ok, nil
}

func boundNonempty(c *httpx.Ctx) bool {
	var raw json.RawMessage
	return c.BindSilent(&raw) && shared.EnabledField(raw, false)
}

func legacy(o object) (*jsonx.Object, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	v, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	out, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("expected object")
	}
	return out, nil
}

func orderPosition(body object) string {
	if !body.Has("position") {
		return "top"
	}
	return text(body, "position")
}
