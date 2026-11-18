package substore

// Compatibility boundaries only. Native owners never call these adapters.
import (
	"configflow/internal/convert"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"math/big"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &result)
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

// SaveSubscriptionNodes transcodes the old cache boundary once per batch.
func SaveSubscriptionNodes(ctx context.Context, id string, nodes any, metadata *jsonx.Object) (*jsonx.Object, error) {
	record, err := decodeLegacy[*Cache](jsonx.Obj("subscription_id", id, "updated_at", "", "count", jsonx.Int(0), "nodes", nodes, "metadata", metadata))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	data, err := json.Marshal(jsonx.Obj("nodes", nodes))
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(data, &raw) == nil {
		count := model.Int(len(raw.Nodes))
		record.Count = &count
	}
	result, err := saveCache(ctx, id, record)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(result)
}

// LoadSubscriptionCache transcodes one typed cache for unported consumers.
func LoadSubscriptionCache(ctx context.Context, id string) (*jsonx.Object, error) {
	result, err := LoadCache(ctx, id)
	if err != nil || result == nil {
		return nil, err
	}
	return encodeLegacy(result)
}

// Status transcodes the lifecycle snapshot for unported response consumers.
func (r *Runtime) Status(ctx context.Context, running bool) (*jsonx.Object, error) {
	value, err := r.Snapshot(ctx, running)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(value)
}

// read/write remain only for old test fixtures; native owners use state records.
func (r *Runtime) read() any {
	value, err := r.loadState()
	if err != nil {
		return nil
	}
	result, _ := encodeLegacy(value)
	return result
}
func (r *Runtime) write(pairs ...any) error {
	value := jsonx.Obj(pairs...)
	patch, err := decodeLegacy[runtimePatch](value)
	if err != nil {
		return err
	}
	return r.writeState(context.Background(), patch)
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

// NewSubscriptionResult is SubscriptionResult(text, source, traffic,
// fetched_at); an empty fetchedAt is now (datetime.now(timezone.utc).isoformat()).
func NewSubscriptionResult(text, source string, traffic *jsonx.Object, fetchedAt string, clock Clock) *SubscriptionResult {
	if fetchedAt == "" {
		fetchedAt = isoFormat(clock.Now())
	}
	return &SubscriptionResult{Text: text, Source: source, Traffic: traffic, FetchedAt: fetchedAt}
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

// ParseProxiesFromYAML transcodes the complete native proxy batch once.
func ParseProxiesFromYAML(text string) ([]any, error) {
	values, err := ParseProxyNodes(text)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(values))
	for _, v := range values {
		data, err := yamlx.ToJSON(v)
		if err != nil {
			return nil, err
		}
		item, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

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

// ParseUserinfo adapts native subscription traffic for unported callers.
func ParseUserinfo(text string) (*jsonx.Object, error) {
	value, err := ReadUserinfo(text)
	if err != nil || value == nil {
		return nil, err
	}
	return encodeLegacy(value)
}

// ParseVersion adapts old dynamic input to the native version parser.
func ParseVersion(value any) []*big.Int {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return VersionParts(text)
}

// IsNewer adapts old dynamic version inputs.
func IsNewer(candidate, current any) bool {
	a, ok := candidate.(string)
	if !ok {
		return false
	}
	b, _ := current.(string)
	return VersionNewer(a, b)
}

// JSON transcodes a native HTTP JSON payload for unported consumers.
func (r *Response) JSON() (any, error) {
	data, err := r.JSONData()
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

// FetchLatestRelease transcodes the selected release at the legacy response boundary.
func (d *Dependencies) FetchLatestRelease(ctx context.Context, force bool) (*jsonx.Object, string) {
	value, message := d.Latest(ctx, force)
	if value == nil {
		return nil, message
	}
	result, err := encodeLegacy(value)
	if err != nil {
		return nil, err.Error()
	}
	return result, message
}

// GetUpdateState transcodes the native observation for old HTTP owners.
func (d *Dependencies) GetUpdateState() *jsonx.Object {
	value, _ := encodeLegacy(d.UpdateSnapshot())
	return value
}

// GetSubStoreStatus transcodes one complete native snapshot.
func (d *Dependencies) GetSubStoreStatus(ctx context.Context, force bool) (*jsonx.Object, error) {
	value, err := d.Snapshot(ctx, force)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(value)
}

// ListDependencies transcodes the fixed native dependency list once per request.
func (d *Dependencies) ListDependencies(ctx context.Context, force bool) ([]any, error) {
	values, err := d.List(ctx, force)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		item, err := encodeLegacy(value)
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
