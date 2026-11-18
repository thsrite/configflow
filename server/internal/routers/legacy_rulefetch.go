package routers

// This codec is removed when the rules owner's SourceConfig API lands. It
// projects rule-fetch inputs once per operation, never once per selected rule.
import (
	"configflow/internal/jsonx"
	"configflow/internal/rulefetch"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type syncEnvironmentKey struct{}
type syncEnvironment struct {
	config *jsonx.Object
	system json.RawMessage
}

func withSyncEnvironment(ctx context.Context, raw json.RawMessage) (context.Context, error) {
	if _, ok := ctx.Value(syncEnvironmentKey{}).(*syncEnvironment); ok {
		return ctx, nil
	}
	// Only the three owner-consumed sections cross the temporary codec.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	selected := map[string]json.RawMessage{}
	for _, key := range []string{"rule_library", "proxy_groups", "system_config"} {
		if v, ok := fields[key]; ok {
			selected[key] = v
		}
	}
	data, err := json.Marshal(selected)
	if err != nil {
		return nil, err
	}
	cfg, err := syncObject(data)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, syncEnvironmentKey{}, &syncEnvironment{cfg, fields["system_config"]}), nil
}
func syncEnvironmentConfig(ctx context.Context, raw json.RawMessage) (*jsonx.Object, error) {
	ctx, err := withSyncEnvironment(ctx, raw)
	if err != nil {
		return nil, err
	}
	return ctx.Value(syncEnvironmentKey{}).(*syncEnvironment).config, nil
}
func syncObject(raw json.RawMessage) (*jsonx.Object, error) {
	var out jsonx.Object
	err := json.Unmarshal(raw, &out)
	return &out, err
}
func syncValidate(ctx context.Context, content string, source syncSource, config json.RawMessage) error {
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return err
	}
	return rulefetch.ValidateRemoteRuleContent(content, rule, cfg, "")
}
func syncCacheRead(ctx context.Context, source syncSource, config json.RawMessage) (string, error) {
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return "", err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return "", err
	}
	path, err := rulefetch.RuleCacheReadPath(ctx, rule, cfg.GetList("rule_library"))
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if source.Source.SourceType == "content" {
			return source.Source.Content, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, syncMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > syncMaxBytes {
		return "", routerError(fmt.Sprintf(messageSyncSourceLarge, orText(source.Source.Name, source.Item.ID)))
	}
	if !utf8.Valid(raw) {
		return "", routerError(fmt.Sprintf(messageSyncUTF8, orText(source.Source.Name, source.Item.ID)))
	}
	return strings.TrimPrefix(string(raw), "\ufeff"), nil
}
func syncCacheWrite(ctx context.Context, source syncSource, content string, config json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return err
	}
	_, err = rulefetch.WriteValidatedRuleCache(ctx, rule, content, cfg)
	return err
}

func syncTransportKey(ctx context.Context, url string) string {
	return routeDigest([]json.RawMessage{mustPolicyJSON(url), ctx.Value(syncEnvironmentKey{}).(*syncEnvironment).system})
}
