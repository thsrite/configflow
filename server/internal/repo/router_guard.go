package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// The router commit guard protects device ownership under the document lock,
// through COMMIT (Python: _router_commit_guard). It compares the router
// records of the committed and the proposed generation member by member as
// stored: a manifest that is malformed is still evidence of ownership (typed
// fields would read it as absent), so the guard stays fail-closed.

type routerKey struct{ ProfileID, RouterID string }

// routerRecordSet is every router of a document by profile and id.
type routerRecordSet struct {
	keys   []routerKey
	values map[routerKey]memberView
}

// routerRecordsIn is _router_records on the typed document.
func routerRecordsIn(document *model.Document) (routerRecordSet, error) {
	records := routerRecordSet{values: map[routerKey]memberView{}}
	for _, profile := range document.Profiles {
		view, err := viewOf(profile)
		if err != nil {
			return records, err
		}
		raw, _ := view.get("routers")
		itemsRaw := json.RawMessage("[]")
		if jsonTruthy(raw) {
			if jsonKind(raw) != '{' {
				return records, validationError("路由器设置必须是对象")
			}
			if value, ok := viewOfJSON(raw).get("items"); ok {
				itemsRaw = value
			}
		}
		routers, err := itemsOf(itemsRaw, "路由器")
		if err != nil {
			return records, err
		}
		for _, id := range routers.ids {
			key := routerKey{profile.Key(), id}
			records.keys = append(records.keys, key)
			records.values[key] = routers.byID[id]
		}
	}
	return records, nil
}

// sameStored reports whether two stored members hold the same JSON value;
// absent and null are the same (Python: .get() is None for both).
func sameStored(a, b json.RawMessage) bool {
	if kindA, kindB := jsonKind(a), jsonKind(b); (kindA == 0 || kindA == 'n') && (kindB == 0 || kindB == 'n') {
		return true
	}
	if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
		return true
	}
	left, err := yamlx.FromJSON(a)
	if err != nil {
		return false
	}
	right, err := yamlx.FromJSON(b)
	return err == nil && yamlx.Equal(left, right)
}

// routerFieldsDiffer is _fields_differ: a missing updated record differs.
func routerFieldsDiffer(old memberView, updated *memberView, fields []string) bool {
	if updated == nil {
		return true
	}
	for _, field := range fields {
		before, _ := old.get(field)
		after, _ := updated.get(field)
		if !sameStored(before, after) {
			return true
		}
	}
	return false
}

// storedValue is a stored member as a jsonx value for usage records; absent
// gives fallback.
func storedValue(view memberView, member string, fallback any) any {
	raw, ok := view.get(member)
	if !ok {
		return fallback
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return fallback
	}
	return value
}

// routerGuard is _router_commit_guard: it refuses changes that would orphan
// a published ownership record and takes the JobLock of every router whose
// connection or manifest changes (once per connection); release frees them.
// previous names the profiles of the old records.
func (r *Repository) routerGuard(ctx context.Context, previous *model.Document, oldRouters, newRouters routerRecordSet,
	lockedRouters map[routerKey]bool, policyRouter *routerKey) (release func(), err error) {
	var changed []routerKey
	for _, key := range oldRouters.keys {
		old := oldRouters.values[key]
		var updated *memberView
		if value, ok := newRouters.values[key]; ok {
			updated = &value
		}
		connectionChanged := routerFieldsDiffer(old, updated, RouterConnectionFields)
		identityChanged := routerFieldsDiffer(old, updated, RouterIdentityFields)
		manifestChanged := routerFieldsDiffer(old, updated, RouterManifestFields)
		// Credentials may have expired on the same device. Keep its exact
		// ownership receipt while allowing access to be repaired; every
		// connection change still takes the publication lock below.
		for _, manifest := range manifestLabels {
			before, _ := old.get(manifest[0])
			fieldChanged := updated == nil
			if updated != nil {
				after, _ := updated.get(manifest[0])
				fieldChanged = !sameStored(before, after)
			}
			isPolicyRouter := policyRouter != nil && *policyRouter == key
			if jsonTruthy(before) && (identityChanged || (fieldChanged && !isPolicyRouter)) {
				var profileName any
				if profile := profileByKey(previous, key.ProfileID); profile != nil {
					if view, err := viewOf(profile); err == nil {
						profileName = storedValue(view, "name", nil)
					}
				}
				return nil, inUse("路由器仍有已发布的"+manifest[1]+"；请先发布并核对撤回，再修改类型、地址或删除发布记录",
					jsonx.Obj("kind", "router", "id", key.RouterID, "name", storedValue(old, "name", key.RouterID),
						"profile_id", key.ProfileID, "profile_name", profileName))
			}
		}
		if connectionChanged || manifestChanged {
			changed = append(changed, key)
		}
	}
	// Never wait for a job while holding the document lock: a publisher may
	// itself be waiting to persist its readback. A failed acquisition rejects
	// the whole transaction, including first publication without a manifest.
	var releases []func()
	release = func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}
	jobLock := currentHooks().JobLock
	connections := map[[2]string]bool{}
	for _, key := range changed {
		if lockedRouters[key] {
			continue
		}
		router := oldRouters.values[key]
		endpoint := ""
		if raw, _ := router.get("endpoint"); jsonTruthy(raw) {
			endpoint = displayJSON(raw)
		}
		typeRaw, _ := router.get("type")
		connection := [2]string{string(bytes.TrimSpace(typeRaw)), strings.TrimRight(endpoint, "/")}
		if connections[connection] {
			continue
		}
		if jobLock == nil {
			release()
			return nil, inUse(msgRouterJobBusy)
		}
		encoded, err := router.fields.MarshalJSON()
		if err != nil {
			release()
			return nil, err
		}
		object, err := legacyRaw(encoded)
		if err != nil {
			release()
			return nil, err
		}
		unlock, acquired := jobLock(ctx, key.ProfileID, key.RouterID, object)
		if !acquired {
			release()
			return nil, inUse(msgRouterJobBusy)
		}
		if unlock != nil {
			releases = append(releases, unlock)
		}
		connections[connection] = true
	}
	return release, nil
}

var manifestLabels = [][2]string{{"ros_policy_state", "策略路由"}, {"dns_guard_state", "强制 DNS 规则"},
	{"ipv6_fakeip_state", "IPv6 Fake-IP 路由"}, {"ipv6_routes_state", "IPv6 规则集路由"},
	{"ipv6_pd_guard_state", "IPv6 前缀变化保护"}}
