package routers

// Rule-source I/O uses the shared typed owner APIs. A source environment is
// decoded once per operation and reused by all selected rules.
import (
	"configflow/internal/model"
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
	config    *rulefetch.SourceConfig
	transport string
}

// withSyncEnvironment scopes one immutable config snapshot to one operation.
// Callers must start from their parent context after replacing the snapshot;
// the derived context is never shared between scheduler rounds or profiles.
func withSyncEnvironment(ctx context.Context, raw json.RawMessage) (context.Context, error) {
	if _, ok := ctx.Value(syncEnvironmentKey{}).(*syncEnvironment); ok {
		return ctx, nil
	}
	var input struct {
		System  json.RawMessage         `json:"system_config"`
		Library []model.RuleLibraryItem `json:"rule_library"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	cfg := &rulefetch.SourceConfig{Library: input.Library}
	if len(input.System) > 0 && string(input.System) != "null" {
		if err := json.Unmarshal(input.System, &cfg.Settings); err != nil {
			return nil, err
		}
	}
	return context.WithValue(ctx, syncEnvironmentKey{}, &syncEnvironment{cfg, routeDigest(input.System)}), nil
}

func syncEnvironmentConfig(ctx context.Context, raw json.RawMessage) (*rulefetch.SourceConfig, error) {
	ctx, err := withSyncEnvironment(ctx, raw)
	if err != nil {
		return nil, err
	}
	return ctx.Value(syncEnvironmentKey{}).(*syncEnvironment).config, nil
}
func syncRule(raw json.RawMessage) (model.RuleConfig, error) {
	var rule model.RuleConfig
	err := json.Unmarshal(raw, &rule)
	return rule, err
}
func syncValidate(ctx context.Context, content string, source syncSource, config json.RawMessage) error {
	rule, err := syncRule(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return err
	}
	return rulefetch.ValidateRemote(rulefetch.Content{Data: []byte(content), Text: true}, rule, cfg.Library, "")
}

// syncCacheRead is a bounded, read-only cache peek: it never migrates a legacy
// path or changes mtime. Batch-8 may provide PeekCacheWithFormat with this
// ownership contract; do not substitute the migrating ReadCache API here.
func syncCacheRead(ctx context.Context, source syncSource, config json.RawMessage) (string, error) {
	rule, err := syncRule(source.Library.Raw)
	if err != nil {
		return "", err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return "", err
	}
	path, err := rulefetch.CacheReadPath(ctx, rule, cfg.Library)
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
	rule, err := syncRule(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return err
	}
	_, err = rulefetch.WriteCache(ctx, rule, rulefetch.Content{Data: []byte(content), Text: true}, cfg)
	return err
}

// writeSyncSources is the service's actual post-readback cache-write loop.
func writeSyncSources(ctx context.Context, sources []syncSource, contents map[string]string, config json.RawMessage) error {
	ctx, err := withSyncEnvironment(ctx, config)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if source.Source.SourceType != "url" || seen[source.Source.ID] {
			continue
		}
		seen[source.Source.ID] = true
		if err = syncCacheWrite(ctx, source, contents[source.Item.ID], config); err != nil {
			return err
		}
	}
	return nil
}
