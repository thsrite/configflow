package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func yamlContent(proxies []proxy) (string, error) {
	data, err := json.Marshal(struct {
		Proxies []proxy `json:"proxies"`
	}{proxies})
	if err != nil {
		return "", err
	}
	// This is program-produced JSON, never user YAML. Build Nodes directly
	// instead of sending that JSON through a second YAML parser.
	root, err := yamlx.FromJSON(data)
	if err != nil {
		return "", err
	}
	data, err = yamlx.Marshal(root)
	return string(data), err
}
func providerResponse(proxies []proxy, id, header string) (*httpx.Response, error) {
	text, err := yamlContent(proxies)
	if err != nil {
		return nil, err
	}
	name := "proxies_"
	if id != "" {
		name += id + "_"
	}
	name += time.Now().Format("20060102_150405") + ".yaml"
	return httpx.Bytes(200, "text/yaml; charset=utf-8; charset=utf-8", []byte(header+text)).WithHeader("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, name)), nil
}

type subscriptionDetail struct {
	name    string
	count   int
	updated string
}

func generatedStamp() string { return time.Now().Format("2006-01-02T15:04:05.000000") }

func singleHeader(name, id string, count int, updated bool, fetchError string) string {
	source := "from local cache"
	if updated {
		source = "from Sub-Store (cache updated)"
	} else if fetchError != "" {
		source += " (fetch error: " + fetchError + ")"
	}
	return fmt.Sprintf("# ConfigFlow Subscription Proxies\n# Subscription: %s\n# ID: %s\n# Generated: %sZ\n# Proxies: %d\n# Source: %s\n\n", name, id, generatedStamp(), count, source)
}

func combinedHeader(details []subscriptionDetail, count int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# ConfigFlow Subscription Proxies\n# Generated: %sZ\n# Subscriptions: %d\n# Total Proxies: %d\n#\n# Subscription Details:\n", generatedStamp(), len(details), count))
	for _, detail := range details {
		b.WriteString(fmt.Sprintf("#   - %s: %d proxies (updated: %s)\n", detail.name, detail.count, detail.updated))
	}
	b.WriteByte('\n')
	return b.String()
}
func providerFailure(ctx context.Context, err error) (*httpx.Response, error) {
	if referenceError(err) {
		return httpx.Message(400, false, err.Error()), nil
	}
	logProviderFailure(ctx, err)
	return httpx.Message(500, false, msgProviderFailed), nil
}
func hasDialer(proxies []proxy) bool {
	for _, proxy := range proxies {
		if raw, ok := proxy["dialer-proxy"]; ok && string(raw) != "null" {
			return true
		}
	}
	return false
}
func (h *Handler) combinedProxies(c *httpx.Ctx) (*httpx.Response, error) {
	if rejected, err := authorize(c); err != nil || rejected != nil {
		return rejected, err
	}
	ctx := materializeContext(h.withRepo(c.Context()))
	delivery, list, err := h.resource(ctx, nil)
	if err != nil {
		return providerFailure(ctx, err)
	}
	if err := delivery.capture(ctx); err != nil {
		return providerFailure(ctx, err)
	}
	all := []proxy{}
	prepared := map[string][]proxy{}
	updates := []staged{}
	details := []subscriptionDetail{}
	for _, sub := range list {
		if !model.Flag(sub.Enabled, true) {
			continue
		}
		raw, result, fetchErr := h.fetchRaw(ctx, sub)
		proxies := []proxy{}
		if fetchErr == nil {
			proxies, err = filter(sub, raw)
			if err != nil {
				return providerFailure(ctx, err)
			}
			update, err := h.stageNodes(sub, raw, result, false)
			if err != nil {
				return providerFailure(ctx, err)
			}
			updates = append(updates, update)
		} else {
			if referenceError(fetchErr) {
				return providerFailure(ctx, fetchErr)
			}
			cached, err := loadCache(ctx, sub.ID)
			if err != nil {
				return providerFailure(ctx, err)
			}
			if cached != nil {
				proxies, err = cachedProxies(ctx, sub, cached.Nodes, true)
				if err != nil {
					return providerFailure(ctx, err)
				}
			}
		}
		proxies, err = delivery.adapt(ctx, sub, proxies, true)
		if err != nil {
			return providerFailure(ctx, err)
		}
		if len(proxies) == 0 {
			continue
		}
		prepared[sub.Name] = proxies
		all = append(all, proxies...)
		details = append(details, subscriptionDetail{sub.Name, len(proxies), generatedStamp()})
	}
	if err := delivery.validate(all, nil); err != nil {
		return providerFailure(ctx, err)
	}
	chains, err := delivery.hasChains()
	if err != nil {
		return providerFailure(ctx, err)
	}
	if chains || hasDialer(all) {
		if err := delivery.validateBundle(ctx, nil, prepared, all); err != nil {
			return providerFailure(ctx, err)
		}
	}
	if err := commitStaged(ctx, updates); err != nil {
		return providerFailure(ctx, err)
	}
	return providerResponse(all, "", combinedHeader(details, len(all)))
}
func (h *Handler) subscriptionProxies(c *httpx.Ctx) (*httpx.Response, error) {
	if rejected, err := authorize(c); err != nil || rejected != nil {
		return rejected, err
	}
	id := c.Param("sub_id")
	delivery, list, err := h.resource(c.Context(), &id)
	if err != nil {
		logProviderFailure(c.Context(), err)
		return httpx.Message(500, false, msgProviderFailed), nil
	}
	index := findSubscription(list, id)
	if index < 0 {
		return httpx.Message(404, false, "Subscription not found"), nil
	}
	filters, err := requestedFilters(c)
	if err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	format, _ := c.Arg("format")
	return delivery.serve(c, id, func(ctx context.Context) (*httpx.Response, error) {
		ctx = materializeContext(h.withRepo(ctx))
		if err := delivery.capture(ctx); err != nil {
			return providerFailure(ctx, err)
		}
		chains, err := delivery.hasChains()
		if err != nil {
			return providerFailure(ctx, err)
		}
		if chains {
			content, err := delivery.chainedContent(ctx, list[index], format)
			if err != nil {
				return providerFailure(ctx, err)
			}
			return httpx.Bytes(200, "text/yaml; charset=utf-8; charset=utf-8", []byte(content)), nil
		}
		return delivery.renderSingle(ctx, list[index], format, filters)
	})
}
func (d *delivery) validateSingle(ctx context.Context, sub model.Subscription, proxies []proxy) error {
	name := sub.Name
	if err := d.validate(proxies, &name); err != nil {
		return err
	}
	if hasDialer(proxies) {
		return d.validateBundle(ctx, &sub, map[string][]proxy{name: proxies}, nil)
	}
	return nil
}
func (d *delivery) renderSingle(ctx context.Context, sub model.Subscription, format string, filters *[2]string) (*httpx.Response, error) {
	alternate := format == "surge" || format == "loon"
	proxies := []proxy(nil)
	cacheUpdated := false
	fetchError := ""
	if sub.URL != "" {
		raw, result, fetchErr := d.h.fetchRaw(ctx, sub)
		if fetchErr == nil {
			proxies, fetchErr = filter(sub, raw)
			if fetchErr == nil {
				proxies, fetchErr = d.adapt(ctx, sub, proxies, !alternate)
			}
			if fetchErr == nil && alternate {
				_, fetchErr = alternative(format, proxies)
			}
			if fetchErr == nil {
				fetchErr = d.validateSingle(ctx, sub, proxies)
			}
			if fetchErr == nil && len(raw) > 0 && result.Source != "cache" {
				update, err := d.h.stageNodes(sub, raw, result, true)
				if err == nil {
					err = commitStaged(ctx, []staged{update})
				}
				if err == nil {
					err = recordFetch(ctx, sub.ID, true, false, len(proxies))
				}
				fetchErr = err
				cacheUpdated = err == nil
			}
		}
		if fetchErr != nil {
			if referenceError(fetchErr) {
				return providerFailure(ctx, fetchErr)
			}
			fetchError = failureDetail(fetchErr)
			proxies = nil
		}
	}
	if proxies == nil {
		cached, err := loadCache(ctx, sub.ID)
		if err != nil {
			return providerFailure(ctx, err)
		}
		if sub.URL != "" {
			count := 0
			if cached != nil {
				_ = json.Unmarshal(cached.Count, &count)
			}
			if err := recordFetch(ctx, sub.ID, false, cached != nil, count); err != nil {
				return providerFailure(ctx, err)
			}
		}
		if cached == nil {
			if fetchError == "" {
				fetchError = msgUnknownError
			}
			return httpx.Message(404, false, fmt.Sprintf(msgProviderNoCache, sub.Name, fetchError)), nil
		}
		if len(cached.Nodes) == 0 {
			return httpx.Message(404, false, fmt.Sprintf(msgEmptyCache, sub.Name)), nil
		}
		// Validate all filtered node shapes before forgiving individual conversion failures.
		filtered, err := filter(sub, cached.Nodes)
		if err != nil {
			return providerFailure(ctx, err)
		}
		proxies, err = cachedProxies(ctx, sub, filtered, false)
		if err != nil {
			return providerFailure(ctx, err)
		}
		proxies, err = d.adapt(ctx, sub, proxies, !alternate)
		if err != nil {
			return providerFailure(ctx, err)
		}
	}
	var alternativeText string
	if alternate {
		var err error
		alternativeText, err = alternative(format, proxies)
		if err != nil {
			return providerFailure(ctx, err)
		}
	}
	if !cacheUpdated {
		if err := d.validateSingle(ctx, sub, proxies); err != nil {
			return providerFailure(ctx, err)
		}
	}
	if alternate {
		if filters != nil {
			filtered, err := filterResponse(proxies, filters)
			if err != nil {
				return providerFailure(ctx, err)
			}
			alternativeText, err = alternative(format, filtered)
			if err != nil {
				return providerFailure(ctx, err)
			}
		}
		return httpx.Text(200, alternativeText), nil
	}
	return providerResponse(proxies, sub.ID, singleHeader(sub.Name, sub.ID, len(proxies), cacheUpdated, fetchError))
}
