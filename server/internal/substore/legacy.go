package substore

// Compatibility boundaries only. Native owners never call these adapters.
import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = jsondecode.Unmarshal(data, &result)
	}
	return result, err
}
func encodeLegacy(value any) (*jsonx.Object, error) {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, _ := decoded.(*jsonx.Object)
	return object, nil
}

// LoadSubscriptionCache transcodes one typed cache for unported consumers.
func LoadSubscriptionCache(ctx context.Context, id string) (*jsonx.Object, error) {
	result, err := LoadCache(ctx, id)
	if err != nil || result == nil {
		return nil, err
	}
	return encodeLegacy(result)
}

// SubscriptionResult is sub_store_client.SubscriptionResult: the target
// format text, its real source (rendered_yaml, direct_yaml, sub_store,
// direct_url_fallback), the traffic read from the same response (nil =
// None) and when it was fetched (ISO time).
type SubscriptionResult struct {
	Text      string
	Source    string
	Traffic   *jsonx.Object
	FetchedAt string
}

// GetSubscriptionProxiesYAML adapts a legacy fetch response at the boundary.
func (c *Client) GetSubscriptionProxiesYAML(ctx context.Context, id, url, target string, opts FetchOptions) (*SubscriptionResult, error) {
	value, err := c.Fetch(ctx, id, url, target, opts)
	if err != nil || value == nil {
		return nil, err
	}
	var traffic *jsonx.Object
	if value.Traffic != nil {
		traffic, err = encodeLegacy(value.Traffic)
		if err != nil {
			return nil, err
		}
	}
	return &SubscriptionResult{Text: value.Text, Source: value.Source, Traffic: traffic, FetchedAt: value.FetchedAt}, nil
}

// ConvertProxyString adapts the old converter interface.
func (c *Client) ConvertProxyString(ctx context.Context, text string) *jsonx.Object {
	return c.ConvertProxyStringTarget(ctx, text, "ClashMeta")
}

// ConvertProxyStringTarget transcodes one native node, leaving conversion logic in ConvertTarget.
func (c *Client) ConvertProxyStringTarget(ctx context.Context, text, target string) *jsonx.Object {
	value, err := c.ConvertTarget(ctx, text, target)
	if err != nil || value == nil {
		return nil
	}
	data, err := yamlx.ToJSON(value)
	if err != nil {
		return nil
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	object, _ := decoded.(*jsonx.Object)
	return object
}

// Converter is the legacy converter used by unported consumers.
func Converter() convert.ProxyStringConverter { return Default().Client }

// ProxiesToNodes transcodes a legacy proxy batch through native records.
func ProxiesToNodes(proxies []any) ([]any, error) {
	values := make([]*yamlx.Node, 0, len(proxies))
	for _, v := range proxies {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		n, err := yamlx.FromJSON(data)
		if err != nil {
			return nil, err
		}
		values = append(values, n)
	}
	nodes, err := ProxyNodes(values)
	if err != nil {
		return nil, err
	}
	data, err := model.Marshal(nodes)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	out, _ := decoded.([]any)
	return out, nil
}

// legacyDependencyStatus retains the response's floating-point timestamp token,
// including idle 0.0. The native observation remains an ordinary float64.
func legacyDependencyStatus(status *DependencyStatus) (*jsonx.Object, error) {
	value, err := encodeLegacy(status)
	if err == nil && value != nil && status != nil {
		value.GetObject("update").Set("finished_at", jsonx.Float(status.Update.FinishedAt))
	}
	return value, err
}

// ListDependencies transcodes the fixed native dependency list once per request.
func (d *Dependencies) ListDependencies(ctx context.Context, force bool) ([]any, error) {
	values, err := d.List(ctx, force)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		item, err := legacyDependencyStatus(value)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// StartSubStoreUpdate adapts the native update error to the old (ok,message) interface.
func (d *Dependencies) StartSubStoreUpdate(ctx context.Context) (bool, string) {
	message, err := d.StartUpdate(ctx)
	if err != nil {
		return false, err.Error()
	}
	return true, message
}
