package aggregations

// Resource handlers call typed repository, validation and provider owners.
import (
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
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

type proxy = generation.Proxy

func stored(value any, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, target)
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
	var fields model.Fields
	if err := stored(body, &fields); err != nil {
		return err
	}
	if err := check.InitProviderHealth(&fields); err != nil {
		return err
	}
	return stored(fields, &body)
}
func readBody(c *httpx.Ctx) (record, error) {
	var raw json.RawMessage
	if err := bindBody(c, &raw); err != nil {
		return nil, err
	}
	var out record
	if json.Unmarshal(raw, &out) != nil {
		return nil, nil
	}
	return out, nil
}
func readOrderBody(c *httpx.Ctx) (record, error) {
	var raw json.RawMessage
	if err := bindBody(c, &raw); err != nil {
		return nil, err
	}
	switch string(raw) {
	case "null", "false", "0", `""`, "[]":
		return record{}, nil
	}
	var out record
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("reorder document must be an object")
	}
	return out, nil
}
func enabled(item record) bool {
	raw, present := item["enabled"]
	if !present {
		return true
	}
	switch string(raw) {
	case "null", "false", "0", `""`, "[]", "{}":
		return false
	}
	return true
}
func resourceConfig(ctx context.Context, id string) (record, record, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	value, err := r.Config(ctx, profileID, repo.ResourceRoots{"subscription_aggregations": []string{id}})
	if err != nil {
		return nil, nil, err
	}
	var config record
	if err := stored(value, &config); err != nil {
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
	result, err := auth.ValidateSystemTokenOrJWT(c, nil)
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
	// The response owner hashes the captured native config JSON directly.
	var profile model.Profile
	if err := stored(config, &profile); err != nil {
		return nil, err
	}
	var shared model.Shared
	if err := stored(config, &shared); err != nil {
		return nil, err
	}
	var system model.System
	if err := stored(config, &system); err != nil {
		return nil, err
	}
	typed := &model.Config{Profile: &profile, ProfileID: config.text("profile_id"), SystemConfig: system.SystemConfig, Backup: system.Backup, Subscriptions: shared.Subscriptions, Nodes: shared.Nodes, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}
	return provider.Serve(c, "aggregation", id, config.text("profile_id"), typed, render)
}
func requestedFilters(c *httpx.Ctx) (*[2]string, error) { return provider.RequestedFilters(c) }
func alternative(format string, proxies []proxy, filters *[2]string) (string, error) {
	var err error
	if filters != nil {
		proxies, err = rules.Filter(proxies, &filters[0], &filters[1], func(item proxy) string { var name string; _ = json.Unmarshal(item["name"], &name); return name })
		if err != nil {
			return "", err
		}
	}
	if proxies == nil {
		proxies = []proxy{}
	}
	raw, err := json.Marshal(proxies)
	if err != nil {
		return "", err
	}
	graph, err := yamlx.FromJSON(raw)
	if err != nil {
		return "", err
	}
	return convert.ClientProxyText(graph.Content, substore.ProxyNodes, format)
}
func resolveOrder(items []record, body record) ([]record, string, error) {
	ids, err := rules.OrderIDs(body, "aggregations")
	if err != nil {
		quoted := "'" + strings.ReplaceAll(strings.ReplaceAll(err.Error(), `\`, `\\`), "'", `\'`) + "'"
		return nil, fmt.Sprintf(msgMissingOrder, "["+quoted+"]"), nil
	}
	ordered, missing := rules.Reorder(items, ids, position(body), func(item record) string { return item.text("id") })
	if len(missing) > 0 {
		quoted := make([]string, len(missing))
		for i, id := range missing {
			quoted[i] = "'" + strings.ReplaceAll(strings.ReplaceAll(id, `\`, `\\`), "'", `\'`) + "'"
		}
		return nil, fmt.Sprintf(msgMissingOrder, "["+strings.Join(quoted, ", ")+"]"), nil
	}
	return ordered, "", nil
}
func failure(err error, message string) *httpx.Response {
	var nonfinite *nonfiniteBodyError
	if errors.As(err, &nonfinite) {
		var request *httpx.HTTPError
		if errors.As(err, &request) {
			return httpx.Message(400, false, request.Description)
		}
	}
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
		cache, err := substore.LoadCache(ctx, id)
		if err != nil {
			return result, err
		}
		if cache == nil {
			continue
		}
		var count json.RawMessage
		if !model.IssueUnder(model.Issues(cache), "nodes") && cache.Nodes != nil {
			var sub model.Subscription
			if item := find(subscriptions, id); item != nil {
				if err := stored(item, &sub); err != nil {
					return result, err
				}
			}
			count, _ = json.Marshal(len(subs.FilterNodes(sub, cache.Nodes)))
		} else {
			// Summary counts retain malformed legacy count tokens in per-source output.
			var fields map[string]json.RawMessage
			if err := stored(cache, &fields); err != nil {
				return result, err
			}
			count = fields["count"]
			if len(count) == 0 {
				count = json.RawMessage("0")
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

func position(body record) string {
	if _, present := body["position"]; !present {
		return "top"
	}
	return body.text("position")
}

// nonfiniteBodyError marks only the native JSON syntax change; malformed
// standard JSON keeps each route's existing handling of Bind's HTTPError.
type nonfiniteBodyError struct{ error }

func (e *nonfiniteBodyError) Unwrap() error { return e.error }
func bindBody(c *httpx.Ctx, target *json.RawMessage) error {
	err := c.Bind(target)
	if err == nil {
		return nil
	}
	if !c.IsJSON() {
		return err
	}
	data, readErr := c.Data()
	if readErr != nil {
		return readErr
	}
	if _, nonfinite := jsonresp.NormalizeNonFinite(data); nonfinite {
		return &nonfiniteBodyError{err}
	}
	return err
}
