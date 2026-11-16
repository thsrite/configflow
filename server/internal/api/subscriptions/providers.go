package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"configflow/internal/httpx"
	"gopkg.in/yaml.v3"
)

func yamlContent(proxies []object) (string, error) {
	data, err := json.Marshal(struct {
		Proxies []object `json:"proxies"`
	}{proxies})
	if err != nil {
		return "", err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", err
	}
	var block func(*yaml.Node)
	block = func(node *yaml.Node) {
		node.Style = 0
		for _, child := range node.Content {
			block(child)
		}
	}
	block(&root)
	data, err = yaml.Marshal(&root)
	return string(data), err
}
func providerResponse(proxies []object, id string) (*httpx.Response, error) {
	text, err := yamlContent(proxies)
	if err != nil {
		return nil, err
	}
	name := "proxies_"
	if id != "" {
		name += id + "_"
	}
	name += time.Now().Format("20060102_150405") + ".yaml"
	return httpx.Bytes(200, "text/yaml; charset=utf-8; charset=utf-8", []byte(text)).WithHeader("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, name)), nil
}
func providerFailure(err error) (*httpx.Response, error) {
	if referenceError(err) {
		return httpx.Message(400, false, err.Error()), nil
	}
	logProviderFailure(err)
	return httpx.Message(500, false, msgProviderFailed), nil
}
func hasDialer(proxies []object) bool {
	for _, proxy := range proxies {
		if raw, ok := proxy["dialer-proxy"]; ok && string(raw) != "null" {
			return true
		}
	}
	return false
}
func combinedProxies(c *httpx.Ctx) (*httpx.Response, error) {
	if rejected, err := authorize(c); err != nil || rejected != nil {
		return rejected, err
	}
	ctx := materializeContext(context.WithoutCancel(c.Context()))
	delivery, list, err := resource(ctx, nil)
	if err != nil {
		return providerFailure(err)
	}
	if err := delivery.capture(ctx); err != nil {
		return providerFailure(err)
	}
	all := []object{}
	prepared := map[string][]object{}
	updates := []staged{}
	for _, sub := range list {
		if !sub.enabled() {
			continue
		}
		raw, result, fetchErr := fetchRaw(ctx, sub)
		proxies := []object{}
		if fetchErr == nil {
			proxies, err = filter(sub, raw)
			if err != nil {
				return providerFailure(err)
			}
			update, err := stageNodes(sub, raw, result, false)
			if err != nil {
				return providerFailure(err)
			}
			updates = append(updates, update)
		} else {
			if referenceError(fetchErr) {
				return providerFailure(fetchErr)
			}
			cached, err := loadCache(ctx, sub.text("id"))
			if err != nil {
				return providerFailure(err)
			}
			if cached != nil {
				proxies, err = cachedProxies(ctx, sub, cached.Nodes, true)
				if err != nil {
					return providerFailure(err)
				}
			}
		}
		proxies, err = delivery.adapt(ctx, sub, proxies, true)
		if err != nil {
			return providerFailure(err)
		}
		if len(proxies) == 0 {
			continue
		}
		prepared[sub.text("name")] = proxies
		all = append(all, proxies...)
	}
	if err := delivery.validate(all, nil); err != nil {
		return providerFailure(err)
	}
	chains, err := delivery.hasChains()
	if err != nil {
		return providerFailure(err)
	}
	if chains || hasDialer(all) {
		if err := delivery.validateBundle(ctx, nil, prepared, all); err != nil {
			return providerFailure(err)
		}
	}
	if err := commitStaged(ctx, updates); err != nil {
		return providerFailure(err)
	}
	return providerResponse(all, "")
}
func subscriptionProxies(c *httpx.Ctx) (*httpx.Response, error) {
	if rejected, err := authorize(c); err != nil || rejected != nil {
		return rejected, err
	}
	id := c.Param("sub_id")
	delivery, list, err := resource(c.Context(), &id)
	if err != nil {
		logProviderFailure(err)
		return httpx.Message(500, false, msgProviderFailed), nil
	}
	sub := findSubscription(list, id)
	if sub == nil {
		return httpx.Message(404, false, "Subscription not found"), nil
	}
	filters, err := requestedFilters(c)
	if err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	format, _ := c.Arg("format")
	return delivery.serve(c, id, func(ctx context.Context) (*httpx.Response, error) {
		ctx = materializeContext(ctx)
		if err := delivery.capture(ctx); err != nil {
			return providerFailure(err)
		}
		chains, err := delivery.hasChains()
		if err != nil {
			return providerFailure(err)
		}
		if chains {
			content, err := delivery.chainedContent(ctx, sub, format)
			if err != nil {
				return providerFailure(err)
			}
			return httpx.Bytes(200, "text/yaml; charset=utf-8; charset=utf-8", []byte(content)), nil
		}
		return delivery.renderSingle(ctx, sub, format, filters)
	})
}
func (d *delivery) validateSingle(ctx context.Context, sub object, proxies []object) error {
	name := sub.text("name")
	if err := d.validate(proxies, &name); err != nil {
		return err
	}
	if hasDialer(proxies) {
		return d.validateBundle(ctx, sub, map[string][]object{name: proxies}, nil)
	}
	return nil
}
func (d *delivery) renderSingle(ctx context.Context, sub object, format string, filters *[2]string) (*httpx.Response, error) {
	alternate := format == "surge" || format == "loon"
	proxies := []object(nil)
	cacheUpdated := false
	fetchError := msgUnknownError
	if sub.text("url") != "" {
		raw, result, fetchErr := fetchRaw(ctx, sub)
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
				update, err := stageNodes(sub, raw, result, true)
				if err == nil {
					err = commitStaged(ctx, []staged{update})
				}
				if err == nil {
					err = recordFetch(ctx, sub.text("id"), true, false, len(proxies))
				}
				fetchErr = err
				cacheUpdated = err == nil
			}
		}
		if fetchErr != nil {
			if referenceError(fetchErr) {
				return providerFailure(fetchErr)
			}
			fetchError = failureDetail(fetchErr)
			proxies = nil
		}
	}
	if proxies == nil {
		cached, err := loadCache(ctx, sub.text("id"))
		if err != nil {
			return providerFailure(err)
		}
		if sub.text("url") != "" {
			count := 0
			if cached != nil {
				_ = json.Unmarshal(cached.Count, &count)
			}
			if err := recordFetch(ctx, sub.text("id"), false, cached != nil, count); err != nil {
				return providerFailure(err)
			}
		}
		if cached == nil {
			return httpx.Message(404, false, fmt.Sprintf(msgProviderNoCache, sub.text("name"), fetchError)), nil
		}
		if len(cached.Nodes) == 0 {
			return httpx.Message(404, false, fmt.Sprintf(msgEmptyCache, sub.text("name"))), nil
		}
		// Validate all filtered node shapes before forgiving individual conversion failures.
		filtered, err := filter(sub, cached.Nodes)
		if err != nil {
			return providerFailure(err)
		}
		proxies, err = cachedProxies(ctx, sub, filtered, false)
		if err != nil {
			return providerFailure(err)
		}
		proxies, err = d.adapt(ctx, sub, proxies, !alternate)
		if err != nil {
			return providerFailure(err)
		}
	}
	var alternativeText string
	if alternate {
		var err error
		alternativeText, err = alternative(format, proxies)
		if err != nil {
			return providerFailure(err)
		}
	}
	if !cacheUpdated {
		if err := d.validateSingle(ctx, sub, proxies); err != nil {
			return providerFailure(err)
		}
	}
	if alternate {
		if filters != nil {
			filtered, err := filterResponse(proxies, filters)
			if err != nil {
				return providerFailure(err)
			}
			alternativeText, err = alternative(format, filtered)
			if err != nil {
				return providerFailure(err)
			}
		}
		return httpx.Text(200, alternativeText), nil
	}
	return providerResponse(proxies, sub.text("id"))
}
