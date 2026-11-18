package discovery

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"context"
	"encoding/json"
	"path/filepath"
)

// ruleRecord projects only this declaration, never a shared/config document.
func ruleRecord(item libraryItem) (model.RuleConfig, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return model.RuleConfig{}, err
	}
	var declaration model.RuleLibraryItem
	if err = json.Unmarshal(raw, &declaration); err != nil {
		return model.RuleConfig{}, err
	}
	declaration.ID, declaration.Name = item.ID, item.Name
	declaration.SourceType = &item.SourceType
	declaration.Behavior, declaration.Content = &item.Behavior, &item.Content
	return rules.LibraryRule(declaration), nil
}
func nativeSaveRule(ctx context.Context, item libraryItem) error {
	rule, err := ruleRecord(item)
	if err != nil {
		return err
	}
	_, err = rulefetch.SaveSource(ctx, rule, nil)
	return err
}
func nativeValidateContent(item libraryItem) error {
	rule, err := ruleRecord(item)
	if err != nil {
		return err
	}
	return rulefetch.ValidateContent(rule, nil)
}
func nativeCachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	rule, err := ruleRecord(item)
	if err != nil {
		return nil, err
	}
	return rulefetch.ReadCache(ctx, rule, nil)
}
func nativeMatcherOptions() rules.CacheOptions {
	return rules.CacheOptions{
		ReadPath: func(rule model.RuleConfig, library []model.RuleLibraryItem, directory string) string {
			unavailable := filepath.Join(directory, rules.CacheFilename(rule)+".invalid", "unavailable")
			ctx := context.Background()
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return unavailable
			}
			var selected string
			err = r.View(ctx, func(view *model.Document) error {
				// Consume borrowed declarations entirely inside View; return
				// only the resulting path, never copy/decode the whole library.
				selected = rules.CacheReadPath(rule, library, view.Shared.RuleLibrary, directory)
				return nil
			})
			if err != nil {
				return unavailable
			}
			return selected
		},
		ValidateRemote: func(content string, rule model.RuleConfig) error {
			return rulefetch.ValidateRemote(rulefetch.Content{Data: []byte(content), Text: true}, rule, nil, "")
		},
	}
}
func nativeLocalMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	view, err := r.Config(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	input := rules.MatcherInput{Rules: view.Profile.RuleConfigs, Library: view.RuleLibrary}
	for _, item := range view.RuleLibrary {
		input.CacheLibrary = append(input.CacheLibrary, model.RuleLibraryItem{ID: item.ID, Name: item.Name})
	}
	matcher := cache(ctx).LocalMatcher(input, id)
	return func(host string) *Match {
		hit := matcher.Match(host)
		if hit == nil {
			return nil
		}
		// The small DTO conversion retains all match extension fields.
		raw, err := json.Marshal(hit)
		if err != nil {
			return nil
		}
		var result Match
		if json.Unmarshal(raw, &result) != nil {
			return nil
		}
		return &result
	}, nil
}
func nativeKnownService(host string) json.RawMessage {
	value := rulefetch.LookupService(host)
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}
func nativePageIdentity(result RegionResult, host, at string) (WebsitePage, error) {
	var status json.RawMessage
	if result.Status != nil {
		status, _ = json.Marshal(*result.Status)
	}
	page := rulefetch.IdentifyPage(rulefetch.PageProbe{Status: status, Error: result.Error, Markers: result.Markers, FinalURL: result.FinalURL}, host, at)
	raw, err := json.Marshal(page)
	if err != nil {
		return WebsitePage{}, err
	}
	var out WebsitePage
	err = json.Unmarshal(raw, &out)
	return out, err
}
