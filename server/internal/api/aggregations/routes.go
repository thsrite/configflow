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
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
)

// Handler serves the /api/aggregations catalog and provider download routes
// backed by explicit services.
type Handler struct {
	Repo         *repo.Repository
	Aggregations *generation.Service
	Provider     *provider.Service
	SubStore     *substore.Service
}

// NewHandler constructs an aggregations Handler. Nil dependencies fall back to
// context/package defaults until W11.
func NewHandler(r *repo.Repository, agg *generation.Service, pv *provider.Service, sub *substore.Service) *Handler {
	return &Handler{Repo: r, Aggregations: agg, Provider: pv, SubStore: sub}
}

// aggregationRequest is an aggregation create/replace request body decoded with
// exact member matching. Display statistics (node_count, total_node_count,
// subscription_node_counts, loading_count) and undeclared members are ignored.
type aggregationRequest struct {
	Name           jsondecode.Member[json.RawMessage] `json:"name"`
	Description    jsondecode.Member[json.RawMessage] `json:"description"`
	Enabled        jsondecode.Member[json.RawMessage] `json:"enabled"`
	Subscriptions  jsondecode.Member[json.RawMessage] `json:"subscriptions"`
	Nodes          jsondecode.Member[json.RawMessage] `json:"nodes"`
	RegexFilter    jsondecode.Member[json.RawMessage] `json:"regex_filter"`
	HealthCheck    jsondecode.Member[json.RawMessage] `json:"health_check"`
	HealthCheckURL jsondecode.Member[json.RawMessage] `json:"health_check_url"`
	CreatedAt      jsondecode.Member[json.RawMessage] `json:"created_at"`
}

// health is the health_check the request stores: as sent, or on creation
// without one the provider defaults (check.ProviderHealthDefaults).
func (r *aggregationRequest) health(creating bool) *model.ProviderHealthCheck {
	if !creating || r.HealthCheck.Present() {
		return nil
	}
	return check.ProviderHealthDefaults(r.HealthCheckURL.Raw)
}

func decodeAggregation(req *aggregationRequest, id, now string, creating bool) (model.Aggregation, error) {
	agg := model.Aggregation{ID: id, UpdatedAt: now}
	if creating {
		agg.CreatedAt = now
	}
	fields := []struct {
		name string
		raw  json.RawMessage
	}{
		{"name", req.Name.Raw},
		{"description", req.Description.Raw},
		{"enabled", req.Enabled.Raw},
		{"subscriptions", req.Subscriptions.Raw},
		{"nodes", req.Nodes.Raw},
		{"regex_filter", req.RegexFilter.Raw},
		{"health_check", req.HealthCheck.Raw},
		{"health_check_url", req.HealthCheckURL.Raw},
	}
	if !creating {
		fields = append(fields, struct {
			name string
			raw  json.RawMessage
		}{"created_at", req.CreatedAt.Raw})
	}
	for _, field := range fields {
		if field.raw == nil {
			continue
		}
		if err := model.DecodeInputMember("aggregation", &agg, field.name, field.raw); err != nil {
			return model.Aggregation{}, err
		}
	}
	if health := req.health(creating); health != nil {
		agg.HealthCheck = health
	}
	return agg, nil
}

func find(items []model.Aggregation, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

// Register installs all eleven aggregation routes on r, including the public alias.
func (h *Handler) Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/aggregations", "subscription_aggregations.handle_subscription_aggregations", auth.Require(h.collection))
	r.Handle([]string{"GET", "PUT", "DELETE"}, "/api/aggregations/<agg_id>", "subscription_aggregations.handle_subscription_aggregation_item", auth.Require(h.item))
	r.GET("/api/aggregations/<agg_id>/count", "subscription_aggregations.get_aggregation_node_count", auth.Require(h.count))
	r.GET("/api/aggregations/<agg_id>/preview", "subscription_aggregations.preview_aggregation_nodes", auth.Require(h.preview))
	r.GET("/api/aggregations/<agg_id>/provider", "subscription_aggregations.get_aggregation_provider", h.download)
	r.GET("/api/aggregations/<resource_id>/health-impact", "subscription_aggregations.health_impact", auth.Require(h.healthImpact))
	r.POST("/api/aggregations/reorder", "subscription_aggregations.reorder_aggregations", auth.Require(h.reorder))
	r.GET("/api/profiles/<profile_id>/aggregations/<agg_id>/provider", "profiles.profile_aggregation_provider", h.download)
}

// Register installs all eleven aggregation routes, including the public alias.
func Register(r *httpx.Router) { NewHandler(nil, nil, nil, nil).Register(r) }

func (h *Handler) collection(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.withRepo(c.Context())
	if c.Method() == http.MethodGet {
		shared, err := generation.LoadCatalog(ctx)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(aggregationsOf(shared)), nil
	}
	if c.Method() != http.MethodPost {
		return nil, errors.New("unsupported aggregation collection method")
	}
	req, ok, err := decodeAggregationBody(c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return httpx.Message(400, false, msgObject), nil
	}
	var entropy [4]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	id := "agg_" + hex.EncodeToString(entropy[:])
	now := time.Now().Format("2006-01-02T15:04:05.000000")
	agg, err := decodeAggregation(req, id, now, true)
	if err != nil {
		return nil, aggregationTypeError(nil, -1, req, id, true, err)
	}
	if err := h.appendAggregation(ctx, agg); err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool              `json:"success"`
		Data    model.Aggregation `json:"data"`
	}{true, agg}), nil
}

func aggregationsOf(shared *model.Shared) []model.Aggregation {
	if shared.Aggregations == nil {
		return []model.Aggregation{}
	}
	return shared.Aggregations
}

func (h *Handler) item(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.withRepo(c.Context())
	shared, err := generation.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if c.Method() != http.MethodGet && c.Method() != http.MethodPut && c.Method() != http.MethodDelete {
		return nil, errors.New("unsupported aggregation item method")
	}
	id := c.Param("agg_id")
	items := shared.Aggregations
	index := find(items, id)
	if c.Method() == http.MethodGet {
		if index < 0 {
			return httpx.Message(404, false, "Aggregation not found"), nil
		}
		return httpx.JSON(items[index]), nil
	}
	if c.Method() == http.MethodDelete {
		kept := make([]model.Aggregation, 0, len(items))
		for _, a := range items {
			if a.ID != id {
				kept = append(kept, a)
			}
		}
		shared.Aggregations = kept
		if err := generation.SaveCatalog(ctx, shared); err != nil {
			return nil, err
		}
		return httpx.JSON(map[string]bool{"success": true}), nil
	}
	if index < 0 {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	req, ok, err := decodeAggregationBody(c)
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	if !ok {
		return httpx.Message(400, false, msgObject), nil
	}
	now := time.Now().Format("2006-01-02T15:04:05.000000")
	agg, err := decodeAggregation(req, id, now, false)
	if err != nil {
		return nil, aggregationTypeError(shared.Aggregations, index, req, id, false, err)
	}
	if err := h.replaceAggregation(ctx, shared, index, agg); err != nil {
		if errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		return failure(err, msgOperationFailed), nil
	}
	return httpx.JSON(struct {
		Success bool              `json:"success"`
		Data    model.Aggregation `json:"data"`
	}{true, agg}), nil
}

func (h *Handler) healthImpact(c *httpx.Ctx) (*httpx.Response, error) {
	return h.resourceUsage(c.Context(), c.Param("resource_id"))
}
func (h *Handler) count(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.withRepo(c.Context())
	shared, err := generation.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	index := find(shared.Aggregations, c.Param("agg_id"))
	if index < 0 {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	result, err := h.cachedCount(ctx, shared, shared.Aggregations[index])
	if err != nil {
		return failure(err, msgCountFailed), nil
	}
	return httpx.JSON(result), nil
}

func (h *Handler) preview(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.withRepo(c.Context())
	config, agg, _, err := h.providerResource(ctx, c.Param("agg_id"))
	if err != nil {
		return nil, err
	}
	if agg == nil {
		return httpx.Message(404, false, "Aggregation not found"), nil
	}
	result, err := h.generateConfig(ctx, *agg, config, generation.Options{Persist: true})
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

func (h *Handler) download(c *httpx.Ctx) (*httpx.Response, error) {
	config, agg, head, err := h.providerResource(c.Context(), c.Param("agg_id"))
	if err != nil {
		return nil, err
	}
	if rejected, err := h.authorize(c); err != nil || rejected != nil {
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
	return h.serve(c, config, head, c.Param("agg_id"), func(ctx context.Context) (*httpx.Response, error) {
		ctx = h.withRepo(ctx)
		result, err := h.generateConfig(ctx, *agg, config, generation.Options{Persist: true, Format: format})
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

func (h *Handler) reorder(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.withRepo(c.Context())
	shared, err := generation.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	body, err := readOrderBody(c)
	if err != nil {
		return failure(err, msgOperationFailed), nil
	}
	ordered, missing := resolveOrder(shared.Aggregations, body)
	if missing != "" {
		return httpx.Message(404, false, missing), nil
	}
	shared.Aggregations = ordered
	if err := generation.SaveCatalog(ctx, shared); err != nil {
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
		ids = append(ids, item.ID)
	}
	return httpx.JSON(struct {
		Success bool     `json:"success"`
		Order   []string `json:"order"`
	}{true, ids}), nil
}
