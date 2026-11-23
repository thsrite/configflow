package subscriptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Register adds the subscriptions blueprint and its profile provider alias.
func Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/subscriptions", "subscriptions.handle_subscriptions", auth.Require(handleSubscriptions))
	r.Handle([]string{"DELETE", "PUT"}, "/api/subscriptions/<sub_id>", "subscriptions.handle_subscription", auth.Require(handleSubscription))
	r.POST("/api/subscriptions/reorder", "subscriptions.reorder_subscriptions", auth.Require(reorderSubscriptions))
	r.GET("/api/subscriptions/health", "subscriptions.subscriptions_health", auth.Require(subscriptionsHealth))
	r.GET("/api/subscriptions/<resource_id>/health-impact", "subscriptions.health_impact", auth.Require(healthImpact))
	r.GET("/api/subscriptions/<sub_id>/nodes", "subscriptions.get_subscription_nodes", auth.Require(subscriptionNodes))
	r.POST("/api/subscriptions/<sub_id>/fetch", "subscriptions.fetch_subscription", auth.Require(fetchSubscription))
	r.GET("/api/subscriptions/test", "subscriptions.test_subscription_route", testRoute)
	r.GET("/api/subscriptions/proxies", "subscriptions.get_all_subscription_proxies", combinedProxies)
	r.GET("/api/subscriptions/<sub_id>/proxies", "subscriptions.get_subscription_proxies", subscriptionProxies)
	r.GET("/api/profiles/<profile_id>/subscriptions/<sub_id>/proxies", "profiles.profile_subscription_proxies", subscriptionProxies)
}
func healthImpact(c *httpx.Ctx) (*httpx.Response, error) {
	return usage(c.Context(), c.Param("resource_id"))
}

// subscriptionWire is a subscription without the stored record's codec, so a
// view can add members next to it.
type subscriptionWire model.Subscription

// subscriptionView is a stored subscription with its derived statistics.
type subscriptionView struct {
	subscriptionWire
	Traffic          json.RawMessage `json:"traffic"`
	TrafficAt        json.RawMessage `json:"traffic_at"`
	LastFetch        json.RawMessage `json:"last_fetch"`
	CachedNodeCount  json.RawMessage `json:"cached_node_count"`
	CachedTotalCount json.RawMessage `json:"cached_total_count"`
	CachedUpdatedAt  json.RawMessage `json:"cached_updated_at"`
}

func handleSubscriptions(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	list, _, err := listShared(ctx)
	if err != nil {
		return nil, err
	}
	if c.Request.Method == "GET" {
		health, err := loadHealth(ctx)
		if err != nil {
			return nil, err
		}
		views := make([]subscriptionView, 0, len(list))
		for _, sub := range list {
			entry := health[sub.ID]
			view := subscriptionView{subscriptionWire: subscriptionWire(sub), Traffic: entry.Traffic, TrafficAt: entry.TrafficAt}
			if len(entry.History) > 0 {
				view.LastFetch = entry.History[len(entry.History)-1]
			}
			cached, err := loadCacheSummary(ctx, sub.ID)
			if err != nil {
				return nil, err
			}
			if cached != nil {
				view.CachedUpdatedAt = cached.UpdatedAt
				if cached.Nodes != nil {
					filtered, err := filter(sub, cached.Nodes)
					if err != nil {
						return nil, err
					}
					view.CachedNodeCount, _ = json.Marshal(len(filtered))
					view.CachedTotalCount, _ = json.Marshal(len(cached.Nodes))
				} else {
					view.CachedNodeCount, view.CachedTotalCount = cached.Count, cached.Count
				}
			}
			views = append(views, view)
		}
		return response(200, views), nil
	}
	if c.Request.Method != "POST" {
		return nil, errors.New("unsupported method")
	}
	sub, err := readBody(c)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, repo.NewValidationError(msgSubscriptionObject)
	}
	sub.stripDerived()
	if err := initialize(sub); err != nil {
		return nil, err
	}
	if err := validateFields(sub); err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	record, err := sub.record()
	if err != nil {
		return nil, err
	}
	err = editSubscriptions(ctx, func(list []model.Subscription) ([]model.Subscription, error) { return append(list, record), nil })
	return response(200, struct {
		Success bool                `json:"success"`
		Data    *subscriptionFields `json:"data"`
	}{true, sub}), err
}
func handleSubscription(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	id := c.Param("sub_id")
	deleting := c.Request.Method == "DELETE"
	// Python checks existence before decoding a PUT body.
	list, _, err := listShared(ctx)
	if err != nil {
		return nil, err
	}
	if !deleting && findSubscription(list, id) < 0 {
		return httpx.Message(404, false, "Subscription not found"), nil
	}
	var body *subscriptionFields
	var record model.Subscription
	if !deleting {
		body, err = readBody(c)
		if err != nil {
			return nil, err
		}
		if body == nil {
			return nil, repo.NewValidationError(msgSubscriptionObject)
		}
		body.stripDerived()
		if err := validateFields(body); err != nil {
			return httpx.Message(400, false, err.Error()), nil
		}
		body.set("id", id)
		if record, err = body.record(); err != nil {
			return nil, err
		}
	}
	changedURL := false
	err = editSubscriptions(ctx, func(list []model.Subscription) ([]model.Subscription, error) {
		result := make([]model.Subscription, 0, len(list))
		replaced := false
		for _, sub := range list {
			if sub.ID != id {
				result = append(result, sub)
				continue
			}
			if deleting {
				continue
			}
			if !replaced {
				changedURL = sub.URL != body.text("url")
				result = append(result, record)
				replaced = true
			} else {
				result = append(result, sub)
			}
		}
		if !deleting && !replaced {
			return nil, &httpx.HTTPError{Code: 404, Description: "Subscription not found"}
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	if deleting {
		if err := forgetSubscription(ctx, id); err != nil {
			return nil, err
		}
		return response(200, struct {
			Success bool `json:"success"`
		}{true}), nil
	}
	if changedURL {
		if err := forgetTask(ctx, id); err != nil {
			return nil, err
		}
	}
	return response(200, struct {
		Success bool                `json:"success"`
		Data    *subscriptionFields `json:"data"`
	}{true, body}), nil
}

type reorderError struct{ missing []string }

func (e *reorderError) Error() string {
	var quoted []string
	for _, id := range e.missing {
		quoted = append(quoted, "'"+strings.ReplaceAll(strings.ReplaceAll(id, `\`, `\\`), "'", `\'`)+"'")
	}
	return fmt.Sprintf(msgMissingIDs, "["+strings.Join(quoted, ", ")+"]")
}
func reorderSubscriptions(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	body, err := readRequestObject(c)
	if err != nil {
		var bad *httpx.HTTPError
		if errors.As(err, &bad) && bad.Code == 400 {
			return httpx.Message(400, false, bad.Description), nil
		}
		return nil, err
	}
	order := []string{}
	err = editSubscriptions(ctx, func(list []model.Subscription) ([]model.Subscription, error) {
		out, missing, err := reorder(list, body)
		if err != nil {
			return nil, err
		}
		if len(missing) > 0 {
			return nil, &reorderError{missing}
		}
		for _, sub := range out {
			order = append(order, sub.ID)
		}
		return out, nil
	})
	if err != nil {
		var missing *reorderError
		var bad *httpx.HTTPError
		if errors.As(err, &bad) && bad.Code == 400 {
			return httpx.Message(400, false, bad.Description), nil
		}
		if errors.As(err, &missing) {
			return httpx.Message(404, false, err.Error()), nil
		}
		if errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		return httpx.Message(500, false, msgOperationFailed), nil
	}
	return response(200, struct {
		Success bool     `json:"success"`
		Order   []string `json:"order"`
	}{true, order}), nil
}

// healthItem is one subscription's fetch history and traffic.
type healthItem struct {
	ID        *string           `json:"id"`
	Name      *string           `json:"name"`
	Traffic   json.RawMessage   `json:"traffic"`
	TrafficAt json.RawMessage   `json:"traffic_at"`
	Enabled   bool              `json:"enabled"`
	History   []json.RawMessage `json:"history"`
}

func subscriptionsHealth(c *httpx.Ctx) (*httpx.Response, error) {
	list, _, err := listShared(c.Context())
	if err != nil {
		return nil, err
	}
	health, err := loadHealth(c.Context())
	if err != nil {
		return nil, err
	}
	items := []healthItem{}
	for _, sub := range list {
		entry := health[sub.ID]
		if entry.History == nil {
			entry.History = []json.RawMessage{}
		}
		items = append(items, healthItem{optional(sub.ID), optional(sub.Name), entry.Traffic, entry.TrafficAt, model.Flag(sub.Enabled, true), entry.History})
	}
	return response(200, struct {
		Success bool         `json:"success"`
		Items   []healthItem `json:"items"`
		Size    int          `json:"size"`
	}{true, items, healthSize()}), nil
}

// optional is a member the record omits when empty: null in a view.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func findSubscription(list []model.Subscription, id string) int {
	for i := range list {
		if list[i].ID == id {
			return i
		}
	}
	return -1
}
func subscriptionNodes(c *httpx.Ctx) (*httpx.Response, error) {
	list, nodes, err := listShared(c.Context())
	if err != nil {
		return nil, err
	}
	id := c.Param("sub_id")
	index := findSubscription(list, id)
	if index < 0 {
		return httpx.Message(404, false, "Subscription not found"), nil
	}
	sub := list[index]
	if source, _ := c.Arg("source"); source == "cache" {
		cached, err := loadCacheSummary(c.Context(), id)
		if err != nil {
			return nil, err
		}
		available := cached != nil && cached.Nodes != nil
		if cached != nil && !cacheURLMatches(cached.Metadata, sub.URL) {
			available = false
		}
		list := []proxy{}
		var updated json.RawMessage
		if available {
			list, err = filter(sub, cached.Nodes)
			if err != nil {
				return nil, err
			}
			updated = cached.UpdatedAt
		}
		return response(200, struct {
			Success   bool            `json:"success"`
			Nodes     []proxy         `json:"nodes"`
			Count     int             `json:"count"`
			Available bool            `json:"cache_available"`
			Updated   json.RawMessage `json:"cached_updated_at"`
		}{true, list, len(list), available, updated}), nil
	}
	selected := []model.Node{}
	for _, node := range nodes {
		if node.SubscriptionID == id {
			selected = append(selected, node)
		}
	}
	return response(200, selected), nil
}
func fetchSubscription(c *httpx.Ctx) (*httpx.Response, error) {
	r, err := fetchSubscriptionData(c)
	if err != nil {
		var request *httpx.HTTPError
		if errors.As(err, &request) || errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		return httpx.Message(500, false, msgOperationFailed), nil
	}
	return r, nil
}

func fetchSubscriptionData(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	body, err := readRequestObject(c)
	if err != nil {
		var bad *httpx.HTTPError
		if errors.As(err, &bad) && bad.Code == 400 {
			return httpx.Message(400, false, bad.Description), nil
		}
		return nil, err
	}
	list, _, err := listShared(ctx)
	if err != nil {
		return nil, err
	}
	index := findSubscription(list, c.Param("sub_id"))
	if index < 0 {
		return httpx.Message(404, false, "Subscription not found"), nil
	}
	sub := list[index]
	nodes, cached, err := refreshSubscription(ctx, sub)
	fromCache := false
	var fetchError *string
	if err != nil {
		detail := failureDetail(err)
		fetchError = &detail
		cached, err = loadCache(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		if cached == nil {
			return httpx.Message(500, false, fmt.Sprintf(msgFetchNoCache, detail)), nil
		}
		nodes = cached.Nodes
		fromCache = true
	}
	nodes, err = filter(sub, nodes)
	if err != nil {
		return nil, err
	}
	reply := fetchReply{Success: true, Count: len(nodes), Nodes: nodes, FromCache: fromCache, FetchError: fetchError, CachedCount: len(nodes), TotalCount: json.RawMessage("0")}
	if cached != nil {
		reply.TotalCount = cached.Count
		reply.CachedUpdatedAt = cached.UpdatedAt
	}
	var request struct {
		Preview jsondecode.Member[json.RawMessage] `json:"preview"`
	}
	if err := jsondecode.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	if nonempty(request.Preview.Raw) {
		reply.Preview = true
	} else {
		added, err := appendImported(ctx, nodes)
		if err != nil {
			return nil, err
		}
		reply.Added = &added
	}
	return response(200, reply), nil
}
func nonempty(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != "false" && string(raw) != "0" && string(raw) != `""` && string(raw) != "[]" && string(raw) != "{}"
}
func testRoute(c *httpx.Ctx) (*httpx.Response, error) {
	return response(200, struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
	}{true, "Subscription route is working!", time.Now().Format("2006-01-02T15:04:05.000000")}), nil
}

func cacheURLMatches(metadata cacheMetadata, url string) bool {
	if metadata.URL.Null() {
		return true
	}
	return metadata.URL.OK && metadata.URL.Value == url
}
