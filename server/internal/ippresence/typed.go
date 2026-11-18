package ippresence

import (
	"configflow/internal/store"
	"context"
	"errors"
	"os"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/rulesetfmt"
)

// TypedClassicalIPLines probes a captured typed declaration using only inline
// content or validated local cache bytes. It never performs a network request.
// A nil result means the content cannot establish whether IP rules are present.
func TypedClassicalIPLines(ctx context.Context, rule model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rule.LibraryRuleID != "" {
		index, err := rulesetfmt.LibraryIndex(rule, library)
		if err != nil {
			return nil, nil
		}
		if index >= 0 {
			item := library[index]
			rule = model.RuleConfig{ID: item.ID, Name: item.Name, SourceType: item.SourceType, URL: item.URL, Content: item.Content, Behavior: item.Behavior, Format: item.Format}
		}
	}
	source := "url"
	if rule.SourceType != nil {
		source = *rule.SourceType
	}
	if source == "content" {
		if rule.Content == nil {
			return nil, nil
		}
		return inlineIPLines(*rule.Content)
	}
	if source != "url" || rule.URL == nil || *rule.URL == "" {
		return nil, nil
	}
	format, err := rulesetfmt.RuleFormat(rule, library, "")
	if err != nil {
		var invalid *rulesetfmt.URLParseError
		if errors.As(err, &invalid) || errors.Is(err, rulesetfmt.ErrURLType) || errors.Is(err, rulesetfmt.ErrReferenceIDType) || errors.Is(err, rulesetfmt.ErrReferenceNameType) {
			return nil, nil
		}
		return nil, err
	}
	if format == "mrs" {
		return nil, nil
	}
	item, config := cacheMetadata(rule, library)
	path, err := rulefetch.RuleCacheReadPath(ctx, item, config.GetList("rule_library"))
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) || errors.Is(err, store.ErrStorage) {
			return nil, nil
		}
		return nil, err
	}
	return cachedIPLines(path, func() ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return rulefetch.ReadCacheWithFormat(ctx, rule, &rulefetch.SourceConfig{Library: library}, format)
	})
}
