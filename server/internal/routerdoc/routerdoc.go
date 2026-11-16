// Package routerdoc holds the router helpers that packages below the router
// core (repo, notifications, agents) need without importing internal/routers:
// backend/routers/store.py routers_of/find_router, all of
// backend/routers/lan_clients.py and the check of
// common/client_fixed_ip.validate_bindings.
//
// Leaf package: jsonx, pystr, pyip only.
package routerdoc

import (
	"configflow/internal/jsonx"
)

// RoutersOf is routers.store.routers_of(config): the dict items of
// config["routers"]["items"], or none when the shape is wrong.
func RoutersOf(config *jsonx.Object) []*jsonx.Object {
	routers, ok := config.Value("routers").(*jsonx.Object)
	if !ok || routers == nil {
		return nil
	}
	items, ok := routers.Value("items").([]any)
	if !ok {
		return nil
	}
	var out []*jsonx.Object
	for _, item := range items {
		if object, ok := item.(*jsonx.Object); ok && object != nil {
			out = append(out, object)
		}
	}
	return out
}

// FindRouter is routers.store.find_router(config, router_id) for a str
// router_id: the first item whose "id" equals it (a non-str id never does).
func FindRouter(config *jsonx.Object, routerID string) *jsonx.Object {
	for _, item := range RoutersOf(config) {
		if id, ok := item.Value("id").(string); ok && id == routerID {
			return item
		}
	}
	return nil
}
