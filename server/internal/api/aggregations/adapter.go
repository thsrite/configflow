package aggregations

// This file isolates legacy repository, auth and converter document types.
import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	generation "configflow/internal/aggregations"
	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
)

type proxy = generation.Proxy

func stored(value any, target any) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, target)
}
func legacy(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	v, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("document must be an object")
	}
	return o, nil
}

func sharedConfig(ctx context.Context) (record, error) {
	raw, err := generation.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var out record
	err = json.Unmarshal(raw, &out)
	return out, err
}
func saveShared(ctx context.Context, document record) error {
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	return generation.SaveCatalog(ctx, raw)
}
func appendAggregation(ctx context.Context, body record) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return generation.AppendCatalog(ctx, raw)
}
func initialize(body record) error {
	o, err := legacy(body)
	if err != nil {
		return err
	}
	check.InitializeProviderHealth(o)
	return stored(o, &body)
}
func readBody(c *httpx.Ctx) (record, error) {
	v, err := c.JSON()
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, nil
	}
	var out record
	err = stored(o, &out)
	return out, err
}
func readOrderBody(c *httpx.Ctx) (record, error) {
	v, err := c.JSON()
	if err != nil {
		return nil, err
	}
	if !jsonx.Truthy(v) {
		return record{}, nil
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("reorder document must be an object")
	}
	var out record
	err = stored(o, &out)
	return out, err
}
func enabled(item record) bool {
	o, err := legacy(item)
	return err == nil && jsonx.Truthy(o.GetOr("enabled", true))
}

func resourceConfig(ctx context.Context, id string) (record, record, error) {
	v, err := repo.GetResourceConfig(ctx, "subscription_aggregations", &id, "")
	if err != nil {
		return nil, nil, err
	}
	var config record
	if err := stored(v, &config); err != nil {
		return nil, nil, err
	}
	items, err := config.list("subscription_aggregations")
	if err != nil {
		return nil, nil, err
	}
	return config, find(items, id), nil
}
func resourceUsage(ctx context.Context, id string) (*httpx.Response, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	v, err := r.GetResourceUsage(ctx, "subscription_aggregations", id)
	return httpx.JSON(v), err
}
func authorize(c *httpx.Ctx) (*httpx.Response, error) {
	result, err := auth.ValidateTokenOrJWT(c, nil)
	if err != nil {
		return nil, err
	}
	if !result.Valid {
		return httpx.Message(401, false, result.Message), nil
	}
	return nil, nil
}

func render(ctx context.Context, agg, config record, format string) (*generation.Result, error) {
	a, err := json.Marshal(agg)
	if err != nil {
		return nil, err
	}
	c, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return generation.Generate(ctx, a, c, generation.Options{Persist: true, Format: format})
}
func serve(c *httpx.Ctx, config record, id string, render provider.Render) (*httpx.Response, error) {
	o, err := legacy(config)
	if err != nil {
		return nil, err
	}
	return provider.ServeProvider(c, "aggregation", id, config.text("profile_id"), o, render)
}
func requestedFilters(c *httpx.Ctx) (*[2]string, error) { return provider.RequestedFilters(c) }
func alternative(format string, proxies []proxy, filters *[2]string) (string, error) {
	raw, err := json.Marshal(proxies)
	if err != nil {
		return "", err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", err
	}
	values := value.([]any)
	if filters != nil {
		values, err = rules.FilterCandidates(values, filters[0], filters[1])
		if err != nil {
			return "", err
		}
	}
	if format == "surge" {
		return convert.ConvertProxiesToSurgeText(values, substore.ProxiesToNodes)
	}
	return convert.ConvertProxiesToLoonText(values, substore.ProxiesToNodes)
}
func resolveOrder(items []record, body record) ([]record, string, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, "", err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, "", err
	}
	o, err := legacy(body)
	if err != nil {
		return nil, "", err
	}
	ordered, missing := rules.ResolveNewOrder(value.([]any), o, "aggregations")
	if len(missing) > 0 {
		values := make([]any, len(missing))
		for i, id := range missing {
			values[i] = id
		}
		return nil, fmt.Sprintf(msgMissingOrder, jsonx.Repr(values)), nil
	}
	var result []record
	err = stored(ordered, &result)
	return result, "", err
}
func failure(err error, message string) *httpx.Response {
	logx.Get("backend.routes.aggregations").Error(message + ": " + weburl.SafeErrorDetails(err))
	return httpx.Message(500, false, message)
}

type countReply struct {
	Success       bool                       `json:"success"`
	Total         json.RawMessage            `json:"total_count"`
	Subscriptions map[string]json.RawMessage `json:"subscription_counts"`
}
type numericCount struct {
	integer  big.Int
	floating bool
	decimal  float64
}

func (n *numericCount) add(raw json.RawMessage) error {
	// Older cache documents may carry a boolean count. Preserve its raw
	// value in the per-source reply while adding its legacy numeric value.
	if string(raw) == "true" {
		raw = json.RawMessage(`1`)
	} else if string(raw) == "false" {
		raw = json.RawMessage(`0`)
	}
	if len(raw) == 0 || raw[0] == '"' {
		return errors.New("cache count must be numeric")
	}
	var value json.Number
	if json.Unmarshal(raw, &value) != nil {
		return errors.New("cache count must be numeric")
	}
	if strings.ContainsAny(value.String(), ".eE") {
		v, err := value.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("cache count must be finite")
		}
		if !n.floating {
			n.decimal, _ = new(big.Float).SetInt(&n.integer).Float64()
			n.floating = true
		}
		n.decimal += v
		return nil
	}
	v, ok := new(big.Int).SetString(value.String(), 10)
	if !ok {
		return errors.New("cache count must be numeric")
	}
	if n.floating {
		f, _ := new(big.Float).SetInt(v).Float64()
		n.decimal += f
	} else {
		n.integer.Add(&n.integer, v)
	}
	return nil
}
func (n *numericCount) raw() (json.RawMessage, error) {
	if n.floating {
		return json.Marshal(n.decimal)
	}
	return json.RawMessage(n.integer.String()), nil
}
func cachedCount(ctx context.Context, shared, aggregation record) (countReply, error) {
	result := countReply{Success: true, Subscriptions: map[string]json.RawMessage{}}
	var ids []string
	if raw := aggregation["subscriptions"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &ids); err != nil {
			return result, err
		}
	}
	subscriptions, err := shared.list("subscriptions")
	if err != nil {
		return result, err
	}
	var total numericCount
	for _, id := range ids {
		cache, err := substore.LoadSubscriptionCache(ctx, id)
		if err != nil {
			return result, err
		}
		if cache == nil {
			continue
		}
		var count json.RawMessage
		if nodes, ok := cache.Value("nodes").([]any); ok {
			var sub *jsonx.Object
			if record := find(subscriptions, id); record != nil {
				sub, err = legacy(record)
				if err != nil {
					return result, err
				}
			}
			count, _ = json.Marshal(len(subs.FilterSubscriptionItems(sub, nodes)))
		} else {
			count, err = jsonx.Compact(cache.GetOr("count", jsonx.Int(0)))
			if err != nil {
				return result, err
			}
		}
		if err := total.add(count); err != nil {
			return result, err
		}
		result.Subscriptions[id] = count
	}
	ids = nil
	if raw := aggregation["nodes"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &ids); err != nil {
			return result, err
		}
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	nodes, err := shared.list("nodes")
	if err != nil {
		return result, err
	}
	manual := 0
	for _, node := range nodes {
		if selected[node.text("id")] && enabled(node) {
			manual++
		}
	}
	raw, _ := json.Marshal(manual)
	if err := total.add(raw); err != nil {
		return result, err
	}
	result.Total, err = total.raw()
	return result, err
}
