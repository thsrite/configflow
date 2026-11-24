package discovery

import (
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"context"
	"encoding/json"
	"errors"
	"os"
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
func (s *Service) nativeSaveRule(ctx context.Context, item libraryItem) error {
	r, err := s.repo()
	if err != nil {
		return err
	}
	rule, err := ruleRecord(item)
	if err != nil {
		return err
	}
	_, err = rulefetch.SaveSource(ctx, r, rule, nil)
	return err
}
func nativeValidateContent(item libraryItem) error {
	rule, err := ruleRecord(item)
	if err != nil {
		return err
	}
	return rulefetch.ValidateContent(rule, nil)
}
func (s *Service) nativeCachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	rule, err := ruleRecord(item)
	if err != nil {
		return nil, err
	}
	return rulefetch.ReadCache(ctx, r, rule, nil)
}
func (s *Service) nativeMatcherOptions() rules.CacheOptions {
	return rules.CacheOptions{
		ReadPath: func(rule model.RuleConfig, library []model.RuleLibraryItem, directory string) string {
			return matcherCachePath(context.Background(), rule, library, directory, func(ctx context.Context, visit func(*model.Document) error) error {
				r, err := s.repo()
				if err != nil {
					return err
				}
				return r.View(ctx, visit)
			})
		},
		ValidateRemote: func(content string, rule model.RuleConfig) error {
			return rulefetch.ValidateRemote(rulefetch.Content{Data: []byte(content), Text: true}, rule, nil, "")
		},
	}
}

// matcherCachePath avoids even opening a View on a stable file. Oversized
// repository documents intentionally are not cached, so consulting View for
// every stable ruleset would otherwise decode the whole configuration again.
func matcherCachePath(ctx context.Context, rule model.RuleConfig, library []model.RuleLibraryItem, directory string, read func(context.Context, func(*model.Document) error) error) string {
	native := filepath.Join(directory, rules.CacheFilename(rule))
	if ctx.Err() != nil {
		return ""
	}
	_, fileErr := os.Stat(native)
	_, receiptErr := os.Lstat(native + ".invalid")
	if fileErr == nil || !errors.Is(receiptErr, os.ErrNotExist) {
		return rules.CacheReadPath(rule, library, nil, directory)
	}
	var selected string
	err := read(ctx, func(document *model.Document) error {
		// Borrowed declarations never escape the read-only callback.
		selected = rules.CacheReadPath(rule, library, document.Shared.RuleLibrary, directory)
		return nil
	})
	if err != nil {
		return ""
	}
	return selected
}

func (s *Service) nativeLocalMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	r, err := s.repo()
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
	matcher := s.cache(ctx).LocalMatcher(input, id)
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

func (s *Service) stageRule(ctx context.Context, item libraryItem) (*rulefetch.CachePublication, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var declaration model.RuleLibraryItem
	if err = json.Unmarshal(raw, &declaration); err != nil {
		return nil, err
	}
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	return rulefetch.StageCache(ctx, r, declaration, []byte(item.Content), nil)
}
