package rules

// Shared source preparation and the combined transaction use the legacy storage
// interfaces only here; composition orchestration remains native.
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
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
	value, err := ruleRecord(library)
	if err != nil {
		return nil, err
	}
	if err := rulefetch.ValidateContent(value, nil); err != nil {
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
	_, _, err = repo.UpdateSharedAndConfigTransaction(ctx, func(tx context.Context, value *jsonx.Object) (*jsonx.Object, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		native, err := native(value)
		if err != nil {
			return nil, err
		}
		if err := sharedEdit(native); err != nil {
			return nil, err
		}
		next, err := legacy(native)
		if err != nil {
			return nil, err
		}
		value.Update(next)
		return nil, nil
	}, func(tx context.Context, id string, before, after *jsonx.Object) (*jsonx.Object, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		old, err := native(before)
		if err != nil {
			return nil, err
		}
		next, err := native(after)
		if err != nil {
			return nil, err
		}
		if err := profileEdit(old, next); err != nil {
			return nil, err
		}
		if err := tx.Err(); err != nil {
			return nil, err
		}
		out, err := legacy(next)
		if err != nil {
			return nil, err
		}
		after.Update(out)
		return nil, nil
	}, repo.Profiles(resolved))
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
