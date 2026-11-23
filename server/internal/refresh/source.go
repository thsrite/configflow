package refresh

import (
	"bytes"
	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

func makeResult(text, source string, traffic *Traffic, at string) *SourceResult {
	if at == "" {
		at = isoMicro(refreshEnv().Clock.Now())
	}
	return &SourceResult{Text: text, Source: source, Traffic: traffic, FetchedAt: at}
}
func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func rawMember[T any](value T, name string) json.RawMessage {
	data, _ := model.Marshal(value)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	return fields[name]
}

// CacheMetadata records only downloaded bodies' hosts and dialer metadata.
func CacheMetadata(ctx context.Context, sub model.Subscription, result *SourceResult) (*SourceMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("subscription fetch returned no result")
	}
	if err := validateSourceYAMLPolicy(result.Text); err != nil {
		return nil, err
	}
	// These nullable members are always present on a newly created receipt.
	var metadata SourceMetadata
	_ = json.Unmarshal([]byte(`{"subscription_name":"","url":null,"fetched_at":null}`), &metadata)
	metadata.SubscriptionName = sub.Name
	metadata.FetchedAt = optionalText(result.FetchedAt)
	if raw := rawMember(sub, "url"); len(raw) > 0 && string(raw) != "null" {
		address := sub.URL
		metadata.URL = &address
	}
	if result.Source != "cache" && result.Source != "prepared_yaml" {
		var err error
		metadata.Hosts, metadata.Dialers, err = refreshEnv().Sources.Metadata(ctx, result.Text)
		if err != nil {
			return nil, err
		}
	}
	return &metadata, nil
}

// DownloadCachePath is the transport-only cache address, separate from nodes.
func DownloadCachePath(address string) string {
	sum := sha256.Sum256([]byte(address))
	return "subscription-downloads/" + hex.EncodeToString(sum[:]) + ".json"
}

// RememberDownload writes a source transport receipt without promoting nodes.
func RememberDownload(ctx context.Context, sub model.Subscription, result *SourceResult) (*SourceResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("subscription fetch returned no result")
	}
	if err := validateSourceYAMLPolicy(result.Text); err != nil {
		return nil, err
	}

	result = makeResult(result.Text, result.Source, result.Traffic, result.FetchedAt)
	data, err := json.Marshal(struct {
		Text      string   `json:"text"`
		FetchedAt string   `json:"fetched_at"`
		Traffic   *Traffic `json:"traffic"`
	}{result.Text, result.FetchedAt, result.Traffic})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := writeSharedData(ctx, DownloadCachePath(sub.URL), data); err != nil {
		return nil, err
	}
	return result, nil
}

// DownloadNodes validates a complete subscription before replacing node bytes.
func DownloadNodes(ctx context.Context, sub model.Subscription) ([]model.Node, *SourceResult, *NodeCache, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if sub.ID == "" {
		return nil, nil, nil, repo.NewValidationError(msgSourceIdentity)
	}
	env := refreshEnv()
	fetched, err := env.Sources.Fetch(ctx, sub.ID, sub.URL, substore.FetchOptions{Refresh: true})
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	result, err := RememberDownload(ctx, sub, fetched)
	if err != nil {
		return nil, nil, nil, err
	}
	nodes, err := env.Sources.ParseNodes(ctx, result.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(nodes) == 0 {
		return nil, nil, nil, &SourceValidationError{Message: msgNoSourceNodes}
	}
	for i := range nodes {
		nodes[i].SubscriptionID = sub.ID
		nodes[i].SubscriptionName = sub.Name
		generated := "node_" + env.RandomHex()
		if len(rawMember(nodes[i], "id")) == 0 {
			nodes[i].ID = generated
		}
	}
	metadata, err := CacheMetadata(ctx, sub, result)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	cache, err := env.Sources.SaveNodes(ctx, sub.ID, nodes, metadata)
	if err != nil {
		return nil, nil, nil, err
	}
	return nodes, result, cache, nil
}

// FetchSourceFn fetches one source within the admitted owner generation.
type FetchSourceFn func(context.Context, string, string) (*SourceResult, error)

// TransportReceipt is the typed read projection of a downloaded source; text
// and fetched_at must be text (text:"strict").
type TransportReceipt struct {
	Text      string   `json:"text" text:"strict"`
	FetchedAt string   `json:"fetched_at" text:"strict"`
	Traffic   *Traffic `json:"traffic,omitzero"`
}

// cacheURL returns the URL the cache was made from. A malformed metadata
// object or a non-null URL that is not text is not an "unknown" legacy URL
// and must not claim a cache.
func cacheURL(cache *NodeCache) (*string, bool) {
	if cache == nil {
		return nil, false
	}
	if cache.MetadataDamaged {
		return nil, true
	}
	if cache.Metadata == nil || cache.Metadata.URL == nil {
		return nil, false
	}
	address := *cache.Metadata.URL
	return &address, false
}

// FetchSource serves current validated nodes, then a fresh transport receipt,
// and only then calls the fetch owner. Reads do not refresh cache timestamps.
func FetchSource(ctx context.Context, sub model.Subscription, fetch FetchSourceFn) (*SourceResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interval, err := CheckInterval(SourceInterval(sub), "")
	if err != nil {
		return nil, &rules.StrategyReferenceError{Message: err.Error()}
	}
	if sub.ID == "" {
		return nil, repo.NewValidationError(msgSourceIdentity)
	}
	env := refreshEnv()
	cache, err := env.Sources.LoadNodes(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	address, malformed := cacheURL(cache)
	if malformed || (address != nil && *address != sub.URL) {
		cache = nil
	}
	usable := cache != nil && len(cache.Nodes) > 0
	current := usable && address != nil && *address == sub.URL
	if interval == 0 || current {
		if !usable {
			name := sub.Name
			if name == "" {
				name = sub.ID
			}
			return nil, &rules.StrategyReferenceError{Message: fmt.Sprintf(msgNoManualCache, name)}
		}
		if text, ok := cacheTexts.lookup(env, sub.ID, cache); ok {
			return makeResult(text, "cache", nil, ""), nil
		}
		if err := convert.ConfigShapes(&model.Config{Profile: &model.Profile{}, Nodes: cache.Nodes}, false); err != nil {
			return nil, &SourceValidationError{Message: err.Error(), Cause: err, CacheShape: true}
		}
		main := yamlx.Map()
		proxies := yamlx.Seq()
		// NodeProxy consumes native stored records and keeps the existing converter
		// collaborator through the baseline's thin adapter, without a Config decode.
		for _, node := range cache.Nodes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			proxy, err := convert.NodeProxy(ctx, node, env.Sources.Converter)
			if err != nil {
				return nil, err
			}
			if proxy != nil && len(proxy.Content) > 0 {
				proxies.Content = append(proxies.Content, proxy)
			}
		}
		yamlx.Set(main, "proxies", proxies)
		data, err := yamlx.Marshal(main)
		if err != nil {
			return nil, err
		}
		text := string(data)
		cacheTexts.store(env, sub.ID, cache, text)
		return makeResult(text, "cache", nil, ""), nil
	}

	if data, err := readSharedData(ctx, DownloadCachePath(sub.URL)); err == nil {
		var receipt TransportReceipt
		if receiptFresh(data, &receipt, interval) {
			if err := validateSourceYAMLPolicy(receipt.Text); err != nil {
				return nil, err
			}
			return makeResult(receipt.Text, "source_cache", receipt.Traffic, receipt.FetchedAt), nil
		}
	} else if !isRepositoryOrOSError(err) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fetch == nil {
		return nil, errors.New("subscription fetch owner unavailable")
	}
	result, err := fetch(ctx, sub.ID, sub.URL)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return RememberDownload(ctx, sub, result)
}

// validateSourceYAMLPolicy runs before any source can replace a cache.
func validateSourceYAMLPolicy(text string) error {
	_, err := yamlx.Parse([]byte(text))
	if errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrAliasBudget) {
		return &repo.Error{Kind: repo.ErrValidation, Message: fmt.Sprintf(msgSourceYAML, err), Cause: err}
	}
	return nil
}

// receiptFresh decodes a transport receipt and reports whether it can be
// served: an object whose text and fetched_at are present text, fetched
// within interval.
func receiptFresh(data []byte, receipt *TransportReceipt, interval int64) bool {
	var members struct {
		Text      json.RawMessage `json:"text"`
		FetchedAt json.RawMessage `json:"fetched_at"`
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(data, &members) != nil {
		return false
	}
	if !isText(members.Text) || !isText(members.FetchedAt) {
		return false
	}
	if _, err := model.DecodeStored(data, receipt, ""); err != nil {
		return false
	}
	return fresh(receipt.FetchedAt, interval)
}

// isText reports a JSON string value.
func isText(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"'
}
