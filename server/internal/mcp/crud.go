package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"
)

var collections = []string{"subscriptions", "nodes", "rules", "rule_library", "proxy_groups"}
var reorderPaths = map[string]string{"subscriptions": "/api/subscriptions/reorder", "nodes": "/api/nodes/reorder", "rules": "/api/rules/reorder", "rule_library": "/api/rule-library/reorder", "proxy_groups": "/api/proxy-groups/reorder"}

func (i *invocation) reorder(ctx context.Context, collection string, ids []string, position string) (json.RawMessage, error) {
	path, ok := reorderPaths[collection]
	if !ok {
		return nil, bad(msgCollection, collection)
	}
	if position != "top" && position != "bottom" {
		return nil, bad(msgPosition)
	}
	if len(ids) == 0 {
		return nil, bad(msgIDsEmpty)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, bad(msgIDsDuplicate)
		}
		seen[id] = true
	}
	rawIDs, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	res, err := i.post(ctx, path, object{"ids": rawIDs, "position": text(position)})
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Collection string          `json:"collection"`
		Position   string          `json:"position"`
		Moved      []string        `json:"moved"`
		Order      json.RawMessage `json:"order"`
	}{collection, position, ids, asObject(res)["order"]})
}
func mergeResource(current, data object, id string) object {
	merged := copyObject(current)
	for k, v := range data {
		merged[k] = v
	}
	merged["id"] = text(id)
	if hc, ok := data["health_check"]; ok && len(hc) > 0 && hc[0] == '{' {
		prev := asObject(current["health_check"])
		previous := current["health_check"]
		if (len(previous) == 0 || previous[0] != '{') && !blankText(current["health_check_url"]) {
			prev["url"] = current["health_check_url"]
		}
		for k, v := range asObject(hc) {
			prev[k] = v
		}
		merged["health_check"] = packed(prev)
	}
	return merged
}
func (i *invocation) crud(ctx context.Context, path, prefix, label string) (json.RawMessage, error) {
	a := i.args
	if err := i.require("action"); err != nil {
		return nil, err
	}
	data := copyObject(a.Data)
	if a.Action == "create" {
		if len(data) == 0 {
			return nil, bad(msgCreate, label)
		}
		if prefix != "" && blankText(data["id"]) {
			existing, err := i.get(ctx, path)
			if err != nil {
				return nil, err
			}
			var rows []object
			_ = json.Unmarshal(existing, &rows)
			taken := map[string]bool{}
			for _, row := range rows {
				taken[rawString(row["id"])] = true
			}
			base := prefix + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
			id := base
			for suffix := 1; taken[id]; suffix++ {
				id = fmt.Sprintf("%s_%d", base, suffix)
			}
			data["id"] = text(id)
		}
		res, err := i.post(ctx, path, data)
		if err != nil {
			return nil, err
		}
		return resultAction("create", "", unwrap(res, packed(data))), nil
	}
	if err := i.require("id"); err != nil {
		return nil, err
	}
	id, err := pathPart(a.ID)
	if err != nil {
		return nil, err
	}
	if a.Action == "delete" {
		_, err := i.delete(ctx, path+"/"+id)
		if err != nil {
			return nil, err
		}
		return resultAction("delete", id, nil), nil
	}
	if a.Action != "update" {
		return nil, bad(msgAction, a.Action)
	}
	if len(data) == 0 {
		return nil, bad(msgUpdate, label)
	}
	if path != "/api/proxy-groups" {
		res, err := i.get(ctx, path)
		if err != nil {
			return nil, err
		}
		var rows []object
		_ = json.Unmarshal(res, &rows)
		found := false
		for _, row := range rows {
			var storedID string
			if json.Unmarshal(row["id"], &storedID) == nil && storedID == id {
				data = mergeResource(row, data, id)
				found = true
				break
			}
		}
		if !found {
			return nil, &apiError{404, fmt.Sprintf(msgNotFound, id)}
		}
	}
	res, err := i.put(ctx, path+"/"+id, data)
	if err != nil {
		return nil, err
	}
	return resultAction("update", id, unwrap(res, packed(data))), nil
}
func (i *invocation) manageRule(ctx context.Context) (json.RawMessage, error) {
	if i.args.Action == "create" {
		if dns, ok := i.args.Data["dns"]; ok {
			if rawString(dns) != "default" {
				return nil, bad(msgCompose)
			}
			i.args.Data = copyObject(i.args.Data)
			delete(i.args.Data, "dns")
		}
	}
	prefix := "rule"
	if rawString(i.args.Data["itemType"]) == "ruleset" {
		prefix = "ruleset"
	}
	result, err := i.crud(ctx, "/api/rules", prefix, "规则")
	if err != nil {
		return nil, err
	}
	if i.args.Action == "create" && value(i.args.Position, "top") == "bottom" {
		o := asObject(result)
		id := rawString(asObject(o["item"])["id"])
		if id != "" {
			if _, err := i.reorder(ctx, "rules", []string{id}, "bottom"); err != nil {
				return nil, err
			}
			o["position"] = text("bottom")
			return packed(o), nil
		}
	}
	return result, nil
}
func validTarget(target string) bool {
	return slices.Contains([]string{"mihomo", "surge", "mosdns", "oxidns"}, target)
}
