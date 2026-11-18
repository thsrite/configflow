package setup

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/routers"
)

func httpURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
		return false
	}
	if port := u.Port(); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return false
		}
	}
	return true
}

func selectableSubscriptions(items []model.Subscription) []model.Subscription {
	out := []model.Subscription{}
	for _, item := range items {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		lines, valid := 0, true
		for _, line := range strings.Split(item.URL, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines++
				valid = valid && httpURL(line)
			}
		}
		if lines > 0 && valid {
			out = append(out, item)
		}
	}
	return out
}

func selectedIDs(fields map[string]json.RawMessage, key string) ([]string, error) {
	if raw, ok := fields[key]; ok {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || items == nil || len(items) > 100 {
			return nil, failure(msgSourceSelection, 400)
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			var id string
			if string(item) == "null" || json.Unmarshal(item, &id) != nil {
				return nil, failure(msgSourceSelection, 400)
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	return []string{}, nil
}

func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (s *Service) source(ctx context.Context, record *Record, fields map[string]json.RawMessage) error {
	if enabled(record.Prepared) {
		return failure(msgSourcePrepared, 409)
	}
	subscriptions, err := selectedIDs(fields, "subscription_ids")
	if err != nil {
		return err
	}
	nodes, err := selectedIDs(fields, "node_ids")
	if err != nil {
		return err
	}
	var sourceURL string
	if raw, ok := fields["url"]; ok && (string(raw) == "null" || json.Unmarshal(raw, &sourceURL) != nil) {
		return failure(msgSubscriptionFormat, 400)
	}
	if len([]rune(sourceURL)) > 8192 {
		return failure(msgSubscriptionFormat, 400)
	}
	if sourceURL = strings.TrimSpace(sourceURL); sourceURL != "" {
		if !httpURL(sourceURL) {
			return failure(msgHTTPSubscription, 400)
		}
		id := "setup_sub_" + record.ID
		if err := updateShared(ctx, s.repository, func(shared *model.Shared) error {
			for i := range shared.Subscriptions {
				if shared.Subscriptions[i].ID == id {
					shared.Subscriptions[i].URL = sourceURL
					return nil
				}
			}
			shared.Subscriptions = append(shared.Subscriptions, model.Subscription{ID: id, Name: record.Name + msgSubscriptionName,
				URL: sourceURL, Enabled: pointer(true), Interval: pointer(model.Int(86400))})
			return nil
		}); err != nil {
			return err
		}
		subscriptions, nodes = []string{id}, []string{}
	}
	shared, err := sharedSnapshot(ctx, s.repository)
	if err != nil {
		return err
	}
	availableSubs, availableNodes, linked := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range shared.Subscriptions {
		availableSubs[item.ID] = item.Enabled == nil || *item.Enabled
	}
	for _, item := range shared.Nodes {
		availableNodes[item.ID] = item.Enabled == nil || *item.Enabled
	}
	for _, item := range selectableSubscriptions(shared.Subscriptions) {
		linked[item.ID] = true
	}
	for _, selection := range []struct {
		ids       []string
		available map[string]bool
	}{{subscriptions, availableSubs}, {nodes, availableNodes}} {
		for _, id := range selection.ids {
			if !selection.available[id] {
				return failure(msgSourceUnavailable, 409)
			}
		}
	}
	for _, id := range subscriptions {
		if !linked[id] {
			return failure(msgUnlinkedSubscription, 400)
		}
	}
	if len(subscriptions) == 0 && len(nodes) == 0 {
		return failure(msgNeedSource, 400)
	}
	record.SubscriptionIDs, record.NodeIDs = unique(subscriptions), unique(nodes)
	record.Step, record.MaxStep = 2, 2
	return nil
}

// UpdateSources validates and saves the selected source identities.
func (s *Service) UpdateSources(ctx context.Context, id string, raw json.RawMessage) (json.RawMessage, error) {
	return s.change(ctx, id, raw, func(held context.Context, record *Record, fields map[string]json.RawMessage) error {
		return s.source(held, record, fields)
	})
}

func (s *Service) change(ctx context.Context, id string, raw json.RawMessage, apply func(context.Context, *Record, map[string]json.RawMessage) error) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := storeFor(s.repository)
	var result json.RawMessage
	err := store.locked(ctx, id, func(held context.Context) error {
		record, err := store.read(held, id)
		if err != nil {
			return err
		}
		if _, err := s.repository.ProfileMetadata(held, record.ProfileID); err != nil {
			return err
		}
		fields, err := bodyFields(raw)
		if err != nil {
			return err
		}
		if err := record.clear("abandoned"); err != nil {
			return err
		}
		if err := apply(held, record, fields); err != nil {
			return err
		}
		if err := store.write(held, record); err != nil {
			return err
		}
		result, err = s.described(held, record)
		return err
	})
	return result, err
}

// SaveRouterDraft keeps router credentials in the private session until a
// successful installation check can attach the actual paired devices.
func (s *Service) SaveRouterDraft(ctx context.Context, id string, raw json.RawMessage) (json.RawMessage, error) {
	return s.change(ctx, id, raw, func(_ context.Context, record *Record, fields map[string]json.RawMessage) error {
		if record.Goal != "router" || !enabled(record.Prepared) || record.MaxStep != 3 {
			return failure(msgRouterDraftPhase, 409)
		}
		value, exists := fields["router"]
		if len(fields) != 1 || !exists {
			return failure(msgRouterDraftFields, 400)
		}
		if string(value) == "null" {
			return record.clear("router_draft")
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(value, &payload) != nil || payload == nil || !allowed(payload, append(append([]string{}, routers.EditableFields...), "password")...) {
			return failure(msgRouterDraftFields, 400)
		}
		var settings struct {
			Proxy string `json:"proxy_gateway_agent_id"`
			DNS   string `json:"dns_agent_id"`
			Auto  bool   `json:"auto_rule_sync"`
		}
		if json.Unmarshal(value, &settings) == nil && (settings.Proxy != "" || settings.DNS != "" || settings.Auto) {
			return failure(msgRouterDraftBinding, 400)
		}
		var previous *routers.Router
		if len(record.RouterDraft) != 0 && string(record.RouterDraft) != "null" {
			previous = &routers.Router{}
			if err := json.Unmarshal(record.RouterDraft, previous); err != nil {
				return err
			}
		}
		draft, err := routers.Normalize(value, previous)
		if err != nil {
			return failure(err.Error(), 400)
		}
		if len(draft.RosPolicyRuleSets) != 0 {
			return failure(msgRouterDraftPolicy, 400)
		}
		record.RouterDraft, err = json.Marshal(draft)
		if err != nil {
			return err
		}
		record.RouterMode = pointer("managed")
		return nil
	})
}
