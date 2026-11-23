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
	"configflow/internal/httpx"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

type proxy = generation.Proxy

func appendAggregation(ctx context.Context, body *aggregationFields) error {
	raw, err := body.MarshalJSON()
	if err != nil {
		return err
	}
	return generation.AppendCatalog(ctx, raw)
}

// replaceAggregation stores the catalog with items[index] replaced by an
// aggregation as sent.
func replaceAggregation(ctx context.Context, shared *model.Shared, index int, body *aggregationFields) error {
	list := make([]json.RawMessage, len(shared.Aggregations))
	for i, item := range shared.Aggregations {
		data, err := model.Marshal(item)
		if i == index {
			data, err = body.MarshalJSON()
		}
		if err != nil {
			return err
		}
		list[i] = data
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	var decoded model.Shared
	if err := decoded.SetJSON("subscription_aggregations", raw); err != nil {
		return repo.InputError(err)
	}
	shared.Aggregations = decoded.Aggregations
	return generation.SaveCatalog(ctx, shared)
}
func readBody(c *httpx.Ctx) (*aggregationFields, error) {
	var raw json.RawMessage
	if err := bindBody(c, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	out := &aggregationFields{}
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, nil
	}
	return out, nil
}

// readOrderBody returns a reorder object; a falsy scalar reads as {}.
func readOrderBody(c *httpx.Ctx) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := bindBody(c, &raw); err != nil {
		return nil, err
	}
	switch string(raw) {
	case "null", "false", "0", `""`, "[]":
		return json.RawMessage("{}"), nil
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, errors.New("reorder document must be an object")
	}
	return raw, nil
}

// providerResource is resourceConfig for the provider download: the typed
// configuration borrowed from the repository snapshot and the aggregation,
// without converting the configuration to generic JSON and back (O10 R9).
// The configuration has the members the provider owner reads (the same ones
// serve and aggregations.Generate used to rebuild from that JSON). head is the
// committed head it was resolved from (the provider flight key).
func providerResource(ctx context.Context, id string) (*model.Config, *model.Aggregation, *store.Head, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	value, head, err := r.ConfigAt(ctx, profileID, repo.ResourceRoots{"subscription_aggregations": []string{id}})
	if err != nil {
		return nil, nil, nil, err
	}
	config := &model.Config{Profile: value.Profile, ProfileID: value.ProfileID, SystemConfig: value.SystemConfig, Backup: value.Backup,
		Subscriptions: value.Subscriptions, Nodes: value.Nodes, Aggregations: value.Aggregations, RuleLibrary: value.RuleLibrary}
	for i := range value.Aggregations {
		if value.Aggregations[i].ID == id {
			agg := value.Aggregations[i]
			return config, &agg, head, nil
		}
	}
	return config, nil, head, nil
}

// aggregationEnabled is enabled() of a typed aggregation's stored member.
func aggregationEnabled(agg *model.Aggregation) (bool, error) {
	raw, present, err := model.EncodeMember(agg, "enabled")
	if err != nil || !present {
		return true, err
	}
	return model.Switch(raw, true), nil
}

func resourceUsage(ctx context.Context, id string) (*httpx.Response, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	v, err := r.ResourceUsage(ctx, "subscription_aggregations", id)
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

func serve(c *httpx.Ctx, config *model.Config, head *store.Head, id string, render provider.Render) (*httpx.Response, error) {
	return provider.Serve(c, "aggregation", id, config.ProfileID, head, render)
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
func resolveOrder(items []model.Aggregation, body json.RawMessage) ([]model.Aggregation, string) {
	request, err := rules.DecodeOrder(body, "aggregations")
	if err != nil {
		return nil, fmt.Sprintf(msgMissingOrder, "["+quoteID(err.Error())+"]")
	}
	ordered, missing := rules.Reorder(items, request.IDs, request.Position, func(item model.Aggregation) string { return item.ID })
	if len(missing) > 0 {
		quoted := make([]string, len(missing))
		for i, id := range missing {
			quoted[i] = quoteID(id)
		}
		return nil, fmt.Sprintf(msgMissingOrder, "["+strings.Join(quoted, ", ")+"]")
	}
	return ordered, ""
}

// quoteID is a single-quoted identifier as the missing-order message shows it.
func quoteID(id string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(id, "\\", "\\\\"), "'", "\\'") + "'"
}
func failure(err error, message string) *httpx.Response {
	var nonfinite *nonfiniteBodyError
	if errors.As(err, &nonfinite) {
		var request *httpx.HTTPError
		if errors.As(err, &request) && request.Code == 400 {
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
func cachedCount(ctx context.Context, shared *model.Shared, aggregation model.Aggregation) (countReply, error) {
	result := countReply{Success: true, Subscriptions: map[string]json.RawMessage{}}
	var total numericCount
	for _, id := range aggregation.Subscriptions {
		cache, err := substore.LoadCache(ctx, id)
		if err != nil {
			return result, err
		}
		if cache == nil {
			continue
		}
		var count json.RawMessage
		if !cache.NodesDamaged() && cache.Nodes != nil {
			var sub model.Subscription
			for _, item := range shared.Subscriptions {
				if item.ID == id {
					sub = item
					break
				}
			}
			count, _ = json.Marshal(len(subs.FilterNodes(sub, cache.Nodes)))
		} else {
			// Summary counts retain malformed legacy count tokens in per-source output.
			count = cache.StoredCount
			if len(count) == 0 && cache.Count != nil {
				count, _ = json.Marshal(int64(*cache.Count))
			}
			if len(count) == 0 {
				count = json.RawMessage("0")
			}
		}
		if err := total.add(count); err != nil {
			return result, err
		}
		result.Subscriptions[id] = count
	}
	selected := map[string]bool{}
	for _, id := range aggregation.Nodes {
		selected[id] = true
	}
	manual := 0
	for _, node := range shared.Nodes {
		if selected[node.ID] && model.Flag(node.Enabled, true) {
			manual++
		}
	}
	raw, _ := json.Marshal(manual)
	if err := total.add(raw); err != nil {
		return result, err
	}
	var err error
	result.Total, err = total.raw()
	return result, err
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
