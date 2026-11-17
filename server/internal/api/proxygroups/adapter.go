package proxygroups

// Legacy collaborators are confined to this boundary. The service retains
// native JSON members and delegates validation and rendering to their owners.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
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
	if jsonx.Truthy(c.JSONSilent()) {
		return object{}, &fault{500, "reorder request must be a JSON object"}
	}
	return object{}, nil
}
func load(ctx context.Context, id string) (object, error) {
	raw, err := repo.GetConfig(ctx, id)
	if err != nil {
		return object{}, err
	}
	return native(raw)
}
func edit(ctx context.Context, id string, fn func(context.Context, *object) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
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
	raw, err := legacy(group)
	if err != nil {
		return object{}, err
	}
	check.InitializeGroupHealth(raw)
	return native(raw)
}
func validate(ctx context.Context, group, profile object, original *object) error {
	g, err := legacy(group)
	if err != nil {
		return err
	}
	p, err := legacy(profile)
	if err != nil {
		return err
	}
	var old *jsonx.Object
	if original != nil {
		old, err = legacy(*original)
		if err != nil {
			return err
		}
	}
	catalog, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return err
	}
	return shared.ValidateCandidates(g, p, catalog, old)
}
func merge(original, patch object) (object, error) {
	a, err := legacy(original)
	if err != nil {
		return object{}, err
	}
	b, err := legacy(patch)
	if err != nil {
		return object{}, err
	}
	out, err := shared.MergeGroupUpdate(a, b)
	if err != nil {
		return object{}, err
	}
	return native(out)
}
func enabled(o object) bool {
	raw, ok := o.Get("enabled")
	if !ok {
		return true
	}
	v, err := jsonx.Parse(raw)
	return err == nil && jsonx.Truthy(v)
}
func incoming(profile object, id string) (bool, error) {
	p, err := legacy(profile)
	if err != nil {
		return false, err
	}
	refs, err := convert.IncomingDialers(p, "group", id)
	return len(refs) > 0, err
}
func relink(profile *object, old string, next *string) ([]string, error) {
	p, err := legacy(*profile)
	if err != nil {
		return nil, err
	}
	changes, err := subs.RelinkGroupListeners(p, old, next)
	if err != nil {
		return nil, err
	}
	*profile, err = native(p)
	return changes, err
}
func validateHealthURL(body object) error {
	raw, _ := body.Get("url")
	var fields object
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	if err := fields.Set("url", raw); err != nil {
		return err
	}
	v, err := legacy(fields)
	if err != nil {
		return err
	}
	return check.ValidateHealth(v, false)
}
func reorder(groups []object, body object) ([]object, []string, error) {
	items := make([]any, len(groups))
	for i, g := range groups {
		v, err := legacy(g)
		if err != nil {
			return nil, nil, err
		}
		items[i] = v
	}
	request, err := legacy(body)
	if err != nil {
		return nil, nil, err
	}
	ordered, missing := shared.ResolveNewOrder(items, request, "groups")
	out := make([]object, len(ordered))
	for i, v := range ordered {
		out[i], err = native(v)
		if err != nil {
			return nil, nil, err
		}
	}
	return out, missing, nil
}
func missingText(ids []string) string {
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = id
	}
	return jsonx.Repr(values)
}
func validateDialers(profile object) error {
	p, err := legacy(profile)
	if err != nil {
		return err
	}
	return convert.ValidateDialers(p)
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
	catalog, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := native(catalog)
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
	v, err := jsonx.Parse(raw)
	return err == nil && jsonx.Truthy(v)
}

func defaults() (struct {
	Group    object `json:"group"`
	Provider object `json:"provider"`
}, error) {
	out := struct {
		Group    object `json:"group"`
		Provider object `json:"provider"`
	}{}
	g := check.GroupDefaults()
	g.Set("tolerance", jsonx.Int(50))
	var err error
	out.Group, err = native(g)
	if err != nil {
		return out, err
	}
	out.Provider, err = native(check.ProviderDefaults())
	return out, err
}
func sharedConfig(ctx context.Context) (object, error) {
	raw, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return object{}, err
	}
	return native(raw)
}

func names(o object, key string) ([]string, error) {
	selection, err := jsonx.Parse(raw(o, key))
	if err != nil {
		return nil, err
	}
	if !jsonx.Truthy(selection) {
		return nil, nil
	}
	entries, ok := selection.([]any)
	if !ok {
		return nil, bad(msgPreviewSelection)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		// Like an unknown string ID, a value that cannot name a stored source
		// has no match. Do not reject the other IDs of a mixed selection.
		if id, ok := entry.(string); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func filterSubscription(sub object, nodes []object) ([]object, error) {
	s, err := legacy(sub)
	if err != nil {
		return nil, err
	}
	rows := make([]any, len(nodes))
	for i, node := range nodes {
		rows[i], err = legacy(node)
		if err != nil {
			return nil, err
		}
	}
	rows = subs.FilterSubscriptionItems(s, rows)
	out := make([]object, len(rows))
	for i, row := range rows {
		out[i], err = native(row)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func filterAggregation(nodes []object, pattern string) ([]object, error) {
	rows := make([]any, len(nodes))
	var err error
	for i, node := range nodes {
		rows[i], err = legacy(node)
		if err != nil {
			return nil, err
		}
	}
	rows, err = shared.FilterCandidates(rows, pattern, "")
	if err != nil {
		return nil, err
	}
	out := make([]object, len(rows))
	for i, row := range rows {
		out[i], err = native(row)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func cachedNodes(ctx context.Context, id string) ([]object, error) {
	cache, err := substore.LoadSubscriptionCache(ctx, id)
	if err != nil {
		return nil, err
	}
	if cache == nil {
		return []object{}, nil
	}
	if _, ok := cache.Value("nodes").([]any); !ok {
		return []object{}, nil
	}
	v, err := native(cache)
	if err != nil {
		return nil, err
	}
	return list(v, "nodes")
}

func readPreviewBody(c *httpx.Ctx) (object, bool, error) {
	body, ok, err := readBody(c, false)
	if err != nil {
		return object{}, false, &fault{500, "invalid JSON request"}
	}
	if !ok && !jsonx.Truthy(c.JSONSilent()) {
		return object{}, true, nil
	}
	return body, ok, nil
}
