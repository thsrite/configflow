package setup

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// resourceNodes keeps cache/filter/conversion decisions with their existing
// owners. Only serialization to the current compatibility APIs lives here.
func resourceNodes(ctx context.Context, fields map[string]json.RawMessage, record *Record) (int, []model.Node, []model.Node, error) {
	count := 0
	output := [][]model.Node{{}, {}}
	for index, selection := range []struct {
		field string
		ids   []string
	}{{"subscriptions", record.SubscriptionIDs}, {"nodes", record.NodeIDs}} {
		var items []json.RawMessage
		if err := json.Unmarshal(fields[selection.field], &items); err != nil {
			return 0, nil, nil, err
		}
		available := map[string]*jsonx.Object{}
		for _, raw := range items {
			var item jsonx.Object
			if err := json.Unmarshal(raw, &item); err != nil {
				return 0, nil, nil, err
			}
			value, present := item.Get("enabled")
			if !present || jsonx.Truthy(value) {
				available[item.GetString("id")] = &item
			}
		}
		for _, id := range selection.ids {
			item := available[id]
			if item == nil {
				return 0, nil, nil, failure(msgPrepareSource, 409)
			}
			nodes := []any{item}
			if index == 0 {
				cache, err := substore.LoadSubscriptionCache(ctx, id)
				if err != nil {
					return 0, nil, nil, err
				}
				if cache == nil {
					cache = jsonx.Obj()
				}
				if value, present := cache.GetObject("metadata").Get("url"); present && !jsonx.Equal(value, item.Value("url")) {
					return 0, nil, nil, failure(msgChangedSourceURL, 400)
				}
				nodes = subs.FilterSubscriptionItems(item, cache.GetList("nodes"))
			}
			for _, entry := range nodes {
				node, ok := jsonx.AsObject(entry)
				if !ok {
					continue
				}
				if enabled, present := node.Get("enabled"); present && !jsonx.Truthy(enabled) {
					continue
				}
				usable, err := convert.ConvertNodeToMihomo(ctx, node, substore.Converter())
				if err != nil {
					return 0, nil, nil, err
				}
				if usable == nil {
					continue
				}
				raw, err := json.Marshal(node)
				if err != nil {
					return 0, nil, nil, err
				}
				var native model.Node
				if err = json.Unmarshal(raw, &native); err != nil {
					return 0, nil, nil, err
				}
				output[index] = append(output[index], native)
				count++
			}
		}
	}
	if count == 0 {
		return 0, nil, nil, failure(msgNoNodes, 400)
	}
	return count, output[0], output[1], nil
}

func validateGenerated(ctx context.Context, candidate *jsonx.Object, baseURL string) error {
	selected := jsonx.NewObject()
	for _, key := range []string{"oxidns", "mosdns", "rule_configs", "rules", "rule_sets", "proxy_groups", "device_notes", "profile_id", "system_config", "rule_library", "agents", "nodes", "subscriptions", "subscription_aggregations"} {
		if value, ok := candidate.Get(key); ok {
			selected.Set(key, value)
		}
	}
	data, err := jsonx.Compact(selected)
	if err != nil {
		return err
	}
	profile, err := oxidns.ProfileFromJSON(data)
	if err != nil {
		return err
	}
	dns, err := oxidns.Generate(profile, baseURL)
	if err != nil {
		return err
	}
	document, err := yamlx.Parse([]byte(dns))
	if err != nil {
		return err
	}
	if _, err = oxidns.RequireValidDocument(document); err != nil {
		return err
	}
	generated, err := convert.GenerateMihomoConfig(ctx, candidate, convert.GenerateOptions{BaseURL: baseURL})
	if err != nil {
		return err
	}
	document, err = yamlx.Parse([]byte(generated))
	if err != nil {
		return err
	}
	for _, key := range []string{"proxy-groups", "rules"} {
		node := yamlx.Resolve(yamlx.Get(document, key))
		if node == nil || len(node.Content) == 0 {
			return failure(msgGeneratedStrategy, 400)
		}
	}
	return nil
}
