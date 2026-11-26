package ippresence

import (
	"context"
	"errors"
	"os"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"configflow/internal/store"
)

// TypedClassicalIPLines probes a captured typed declaration using only inline
// content or validated local cache bytes. It never performs a network request.
// A nil result means the content cannot establish whether IP rules are present.
func TypedClassicalIPLines(ctx context.Context, r *repo.Repository, rule model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
	return typedClassicalIPLines(ctx, r, rule, library, rulefetch.ReadCacheWithFormat)
}

// PeekTypedClassicalIPLines probes offline metadata for pure generation. Unlike
// the request reader, it never migrates a validated cache under a legacy name.
func PeekTypedClassicalIPLines(ctx context.Context, r *repo.Repository, rule model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
	return typedClassicalIPLines(ctx, r, rule, library, rulefetch.PeekCacheWithFormat)
}

type cacheReader func(context.Context, *repo.Repository, model.RuleConfig, *rulefetch.SourceConfig, string) ([]byte, error)

func typedClassicalIPLines(ctx context.Context, r *repo.Repository, rule model.RuleConfig, library []model.RuleLibraryItem, read cacheReader) (*bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rule.LibraryRuleID != "" {
		for i := range library {
			if library[i].ID == rule.LibraryRuleID {
				rule = rules.LibraryRule(library[i])
				break
			}
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
	if r == nil {
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
	path, err := rulefetch.CacheReadPath(ctx, r, rule, library)
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
		return read(ctx, r, rule, &rulefetch.SourceConfig{Library: library}, format)
	})
}
