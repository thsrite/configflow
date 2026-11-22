package rules

// Shared source preparation uses native source and repository owners.
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

func prepareLibrary(raw json.RawMessage) (record, error) {
	var library record
	if json.Unmarshal(raw, &library) != nil || library == nil {
		return nil, invalid(msgLibrarySource)
	}
	library = library.clone()
	for _, key := range []string{"id", "base_url", "has_ip_part"} {
		delete(library, key)
	}
	name, ok := textValue(library["name"])
	if !ok || strings.TrimSpace(name) == "" {
		return nil, invalid(msgLibraryName)
	}
	library["name"] = stringJSON(strings.TrimSpace(name))
	library["enabled"] = boolJSON(notFalse(library["enabled"]))
	sent, err := json.Marshal(library)
	if err != nil {
		return nil, err
	}
	if err := rulefetch.ValidateContentJSON(sent, nil); err != nil {
		return nil, err
	}
	if defaultText(library, "source_type", "url") == "url" {
		delete(library, "content")
	} else {
		delete(library, "url")
	}
	return library, nil
}
func (s *Service) prepareRemote(ctx context.Context, library record) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := ruleRecord(library)
	if err != nil {
		return nil, err
	}
	content, err := rulefetch.ReadSource(ctx, value, s.client)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		if rulefetch.IsRequestException(err) {
			return nil, &fault{502, msgDownloadFailed}
		}
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	return content.Data, nil
}
func (s *Service) cacheComposed(ctx context.Context, library record, prepared []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := ruleRecord(library)
	if err != nil {
		return err
	}
	if prepared != nil {
		_, err = rulefetch.WriteCache(ctx, value, rulefetch.Content{Data: prepared}, nil)
	} else {
		_, err = rulefetch.SaveSource(ctx, value, s.client)
	}
	return err
}
func editSharedProfile(ctx context.Context, id string, sharedEdit func(record) error, profileEdit func(record, record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := repo.ResolveProfileID(ctx, id)
	if err != nil {
		return err
	}
	err = repo.UpdateSharedConfigs(ctx, func(tx context.Context, value *model.Shared) error {
		if err := tx.Err(); err != nil {
			return err
		}
		catalog, err := native(value)
		if err != nil {
			return err
		}
		before, err := native(value)
		if err != nil {
			return err
		}
		if err := sharedEdit(catalog); err != nil {
			return err
		}
		if err := applyFields(before, catalog, repo.SharedFields, value.SetJSON); err != nil {
			return err
		}
		return tx.Err()
	}, func(tx context.Context, _ string, before, after *model.Config) error {
		if err := tx.Err(); err != nil {
			return err
		}
		old, err := native(before)
		if err != nil {
			return err
		}
		next, err := native(after)
		if err != nil {
			return err
		}
		baseline, err := native(after)
		if err != nil {
			return err
		}
		if err := profileEdit(old, next); err != nil {
			return err
		}
		if err := applyFields(baseline, next, repo.ProfileFields, after.Profile.SetJSON); err != nil {
			return err
		}
		return tx.Err()
	}, func(*model.Shared, []*model.Profile) ([]string, error) { return []string{resolved}, nil })

	return err
}
func libraryInUse(id string, index int) error {
	return repo.NewInUse(fmt.Sprintf(msgLibraryUsed, index), []any{struct {
		Kind  string `json:"kind"`
		Code  string `json:"code"`
		ID    string `json:"id"`
		Index int    `json:"index"`
	}{"rule", "ruleset_already_used", id, index}})
}
