// Package aggregations serves the shared aggregation catalog and profile-scoped
// provider downloads. Rendering and atomic publication belong to aggregations.
package aggregations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	generation "configflow/internal/aggregations"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

type record map[string]json.RawMessage

func (r record) text(key string) string { var s string; _ = json.Unmarshal(r[key], &s); return s }
func (r record) set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err == nil {
		r[key] = raw
	}
	return err
}
func (r record) list(key string) ([]record, error) {
	values := []record{}
	if raw := r[key]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
	}
	return values, nil
}
func find(items []record, id string) record {
	for _, item := range items {
		if item.text("id") == id {
			return item
		}
	}
	return nil
}
func stripStats(item record, loading bool) {
	for _, key := range []string{"node_count", "total_node_count", "subscription_node_counts"} {
		delete(item, key)
	}
	if loading {
		delete(item, "loading_count")
	}
}

// Register installs all eleven aggregation routes, including the public alias.
func Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/aggregations", "subscription_aggregations.handle_subscription_aggregations", auth.Require(collection))
	r.Handle([]string{"GET", "PUT", "DELETE"}, "/api/aggregations/<agg_id>", "subscription_aggregations.handle_subscription_aggregation_item", auth.Require(item))
	r.GET("/api/aggregations/<agg_id>/count", "subscription_aggregations.get_aggregation_node_count", auth.Require(count))
	r.GET("/api/aggregations/<agg_id>/preview", "subscription_aggregations.preview_aggregation_nodes", auth.Require(preview))
	r.GET("/api/aggregations/<agg_id>/provider", "subscription_aggregations.get_aggregation_provider", download)
	r.GET("/api/aggregations/<resource_id>/health-impact", "subscription_aggregations.health_impact", auth.Require(healthImpact))
	r.POST("/api/aggregations/reorder", "subscription_aggregations.reorder_aggregations", auth.Require(reorder))
	r.GET("/api/profiles/<profile_id>/aggregations/<agg_id>/provider", "profiles.profile_aggregation_provider", download)
}

func collection(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	shared, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	items, err := shared.list("subscription_aggregations")
	if err != nil {
		return nil, err
	}
	if c.Method() == http.MethodGet {
		return httpx.JSON(items), nil
	}
	if c.Method() != http.MethodPost {
		return nil, errors.New("unsupported aggregation collection method")
	}
	body, err := readBody(c)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgObject), nil
	}
	if err := initialize(body); err != nil {
		return nil, err
	}
	var entropy [4]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	if err := body.set("id", "agg_"+hex.EncodeToString(entropy[:])); err != nil {
		return nil, err
	}
	if err := body.set("created_at", time.Now().Format("2006-01-02T15:04:05.000000")); err != nil {
		return nil, err
	}
	if err := body.set("updated_at", time.Now().Format("2006-01-02T15:04:05.000000")); err != nil {
		return nil, err
	}
	stripStats(body, true)
	if err := appendAggregation(ctx, body); err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool   `json:"success"`
		Data    record `json:"data"`
	}{true, body}), nil
}

func item(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	shared, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	items, err := shared.list("subscription_aggregations")
	if err != nil {
		return nil, err
	}
	if c.Method() != http.MethodGet && c.Method() != http.MethodPut && c.Method() != http.MethodDelete {
		return nil, errors.New("unsupported aggregation item method")
	}
	id := c.Param("agg_id")
	current := find(items, id)
	if c.Method() == http.MethodGet {
		if current == nil {
			return httpx.Message(404, false, "Aggregation not found"), nil
		}
		stripStats(current, false)
		return httpx.JSON(current), nil
	}
	if c.Method() == http.MethodDelete {
		kept := make([]record, 0, len(items))
		for _, a := range items {
			if a.text("id") != id {
				kept = append(kept, a)
			}
		}
		if err := shared.set("subscription_aggregations", kept); err != nil {
			return nil, err
		}
		if err := saveShared(ctx, shared); err != nil {
			return nil, err
		}
		return httpx.JSON(map[string]bool{"success": true}), nil
	}
	if current == nil {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	body, err := readBody(c)
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	if body == nil {
		return httpx.Message(400, false, msgObject), nil
	}
	if err := body.set("id", id); err != nil {
		return nil, err
	}
	if err := body.set("updated_at", time.Now().Format("2006-01-02T15:04:05.000000")); err != nil {
		return nil, err
	}
	stripStats(body, true)
	for i, a := range items {
		if a.text("id") == id {
			items[i] = body
			break
		}
	}
	if err := shared.set("subscription_aggregations", items); err != nil {
		return nil, err
	}
	if err := saveShared(ctx, shared); err != nil {
		if errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		return failure(err, msgOperationFailed), nil
	}
	return httpx.JSON(struct {
		Success bool   `json:"success"`
		Data    record `json:"data"`
	}{true, body}), nil
}

func healthImpact(c *httpx.Ctx) (*httpx.Response, error) {
	return resourceUsage(c.Context(), c.Param("resource_id"))
}
func count(c *httpx.Ctx) (*httpx.Response, error) {
	shared, err := sharedConfig(c.Context())
	if err != nil {
		return nil, err
	}
	items, err := shared.list("subscription_aggregations")
	if err != nil {
		return failure(err, msgCountFailed), nil
	}
	agg := find(items, c.Param("agg_id"))
	if agg == nil {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	result, err := cachedCount(c.Context(), shared, agg)
	if err != nil {
		return failure(err, msgCountFailed), nil
	}
	return httpx.JSON(result), nil
}

func preview(c *httpx.Ctx) (*httpx.Response, error) {
	config, agg, err := resourceConfig(c.Context(), c.Param("agg_id"))
	if err != nil {
		return nil, err
	}
	if agg == nil {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	result, err := render(c.Context(), agg, config, "")
	if err != nil {
		return renderFailure(err), nil
	}
	return httpx.JSON(struct {
		Success bool           `json:"success"`
		Count   int            `json:"count"`
		Nodes   []proxy        `json:"nodes"`
		Counts  map[string]int `json:"subscription_node_counts"`
	}{true, len(result.Proxies), result.Proxies, result.SubscriptionNodeCounts}), nil
}

func download(c *httpx.Ctx) (*httpx.Response, error) {
	config, agg, err := providerResource(c.Context(), c.Param("agg_id"))
	if err != nil {
		return nil, err
	}
	if rejected, err := authorize(c); err != nil || rejected != nil {
		return rejected, err
	}
	if agg == nil {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	if on, err := aggregationEnabled(agg); err != nil {
		return nil, err
	} else if !on {
		return httpx.Message(403, false, "Aggregation is disabled"), nil
	}
	filters, err := requestedFilters(c)
	if err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	format, _ := c.Arg("format")
	return serve(c, config, c.Param("agg_id"), func(ctx context.Context) (*httpx.Response, error) {
		result, err := generation.GenerateConfig(ctx, *agg, config, generation.Options{Persist: true, Format: format})
		if err != nil {
			return renderFailure(err), nil
		}
		if format == "surge" || format == "loon" {
			text, err := alternative(format, result.Proxies, filters)
			if err != nil {
				return renderFailure(err), nil
			}
			return httpx.Bytes(200, "text/plain; charset=utf-8", []byte(text)), nil
		}
		return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(result.Content)), nil
	})
}

func renderFailure(err error) *httpx.Response {
	var reference *rules.StrategyReferenceError
	if errors.As(err, &reference) {
		return httpx.Message(400, false, err.Error())
	}
	return failure(err, msgOperationFailed)
}

func reorder(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	shared, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	body, err := readOrderBody(c)
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	items, err := shared.list("subscription_aggregations")
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	ordered, missing, err := resolveOrder(items, body)
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	if missing != "" {
		return httpx.Message(404, false, missing), nil
	}
	if err := shared.set("subscription_aggregations", ordered); err != nil {
		return nil, err
	}
	if err := saveShared(ctx, shared); err != nil {
		if errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		var ref *rules.StrategyReferenceError
		if errors.As(err, &ref) {
			return httpx.Message(400, false, err.Error()), nil
		}
		return failure(err, msgOperationFailed), nil
	}
	ids := make([]string, 0, len(ordered))
	for _, item := range ordered {
		ids = append(ids, item.text("id"))
	}
	return httpx.JSON(struct {
		Success bool     `json:"success"`
		Order   []string `json:"order"`
	}{true, ids}), nil
}
