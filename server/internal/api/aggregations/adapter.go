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

func (h *Handler) repo(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.Repo != nil {
		return h.Repo, nil
	}
	return repo.GetRepository(ctx)
}

func (h *Handler) withRepo(ctx context.Context) context.Context {
	if h != nil && h.Repo != nil {
		return repo.WithRepository(ctx, h.Repo)
	}
	return ctx
}

func (h *Handler) generateConfig(ctx context.Context, agg model.Aggregation, config *model.Config, options generation.Options) (*generation.Result, error) {
	if h != nil && h.Aggregations != nil {
		return h.Aggregations.GenerateConfig(ctx, agg, config, options)
	}
	return generation.GenerateConfig(ctx, agg, config, options)
}

func (h *Handler) appendAggregation(ctx context.Context, agg model.Aggregation) error {
	raw, err := model.Marshal(agg)
	if err != nil {
		return err
	}
	return generation.AppendCatalog(h.withRepo(ctx), raw)
}

// replaceAggregation stores the catalog with items[index] replaced by agg.
func (h *Handler) replaceAggregation(ctx context.Context, shared *model.Shared, index int, agg model.Aggregation) error {
	shared.Aggregations[index] = agg
	return generation.SaveCatalog(h.withRepo(ctx), shared)
}

type aggregationErrorItem struct {
	ID            string          `json:"id"`
	Name          json.RawMessage `json:"name,omitempty"`
	HealthCheck   json.RawMessage `json:"health_check,omitempty"`
	Subscriptions json.RawMessage `json:"subscriptions,omitempty"`
	Nodes         json.RawMessage `json:"nodes,omitempty"`
}

// aggregationTypeError reports a sent member the aggregation record cannot
// hold with the repository's message for the resulting list. The failed item
// carries the members that message is chosen from, including the default
// health_check a creation adds (its legacy health_check_url is validated as
// the probe URL first, as before).
func aggregationTypeError(existing []model.Aggregation, index int, req *aggregationRequest, id string, creating bool, cause error) error {
	health := req.HealthCheck.Raw
	if defaults := req.health(creating); defaults != nil {
		var err error
		if health, err = json.Marshal(defaults); err != nil {
			return err
		}
	}
	failed, err := json.Marshal(aggregationErrorItem{
		ID:            id,
		Name:          req.Name.Raw,
		HealthCheck:   health,
		Subscriptions: req.Subscriptions.Raw,
		Nodes:         req.Nodes.Raw,
	})
	if err != nil {
		return err
	}
	list := make([]json.RawMessage, 0, len(existing)+1)
	for i, item := range existing {
		if i == index {
			list = append(list, failed)
			continue
		}
		data, err := model.Marshal(item)
		if err != nil {
			return err
		}
		list = append(list, data)
	}
	if index < 0 {
		list = append(list, failed)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return repo.InputError(&model.MemberTypeError{Record: "shared", Member: "subscription_aggregations", Value: raw, Err: cause})
}

func decodeAggregationBody(c *httpx.Ctx) (*aggregationRequest, bool, error) {
	req, ok, err := httpx.BindObject[aggregationRequest](c, false)
	if err != nil {
		return nil, false, wrapNonFinite(c, err)
	}
	if !ok {
		return nil, false, nil
	}
	return &req, true, nil
}

// readOrderBody returns a reorder object; a falsy scalar reads as {}.
func readOrderBody(c *httpx.Ctx) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return nil, wrapNonFinite(c, err)
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
func (h *Handler) providerResource(ctx context.Context, id string) (*model.Config, *model.Aggregation, *store.Head, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	profileID, err := repo.ResolveProfileID(h.withRepo(ctx), "")
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := h.repo(ctx)
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

func (h *Handler) resourceUsage(ctx context.Context, id string) (*httpx.Response, error) {
	r, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	v, err := r.ResourceUsage(ctx, "subscription_aggregations", id)
	return httpx.JSON(v), err
}
func (h *Handler) authorize(c *httpx.Ctx) (*httpx.Response, error) {
	r, err := h.repo(c.Context())
	if err != nil {
		return nil, err
	}
	result, err := auth.ValidateSystemTokenOrJWT(c, r, nil)
	if err != nil {
		return nil, err
	}
	if !result.Valid {
		return httpx.Message(401, false, result.Message), nil
	}
	return nil, nil
}

func (h *Handler) serve(c *httpx.Ctx, config *model.Config, head *store.Head, id string, render provider.Render) (*httpx.Response, error) {
	if h != nil && h.Provider != nil {
		return h.Provider.Serve(c, "aggregation", id, config.ProfileID, head, render)
	}
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
func (h *Handler) cachedCount(ctx context.Context, shared *model.Shared, aggregation model.Aggregation) (countReply, error) {
	result := countReply{Success: true, Subscriptions: map[string]json.RawMessage{}}
	var total numericCount
	for _, id := range aggregation.Subscriptions {
		cache, err := substore.LoadCache(h.withRepo(ctx), id)
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
func wrapNonFinite(c *httpx.Ctx, err error) error {
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
