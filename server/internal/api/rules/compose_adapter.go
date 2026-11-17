package rules

// Shared source preparation and the combined transaction use the legacy storage
// interfaces only here; composition orchestration remains native.
import (
	"context"
	"encoding/json"
	"errors"
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
	value, err := legacy(library)
	if err != nil {
		return nil, err
	}
	if err := rulefetch.ValidateDomainContent(value, nil); err != nil {
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
	value, err := legacy(library)
	if err != nil {
		return nil, err
	}
	content, err := rulefetch.ReadRuleSourceContent(ctx, value, s.client)
	if err != nil {
		if rulefetch.IsRequestException(err) {
			return nil, &fault{502, msgDownloadFailed}
		}
		return nil, err
	}
	switch content := content.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(content), nil
	case []byte:
		return content, nil
	default:
		return nil, errors.New("invalid rule source bytes")
	}
}
func (s *Service) cacheComposed(ctx context.Context, library record, prepared []byte) error {
	value, err := legacy(library)
	if err != nil {
		return err
	}
	if prepared != nil {
		_, err = rulefetch.WriteValidatedRuleCache(ctx, value, prepared, nil)
	} else {
		_, err = rulefetch.SaveRuleToLocal(ctx, value, s.client)
	}
	return err
}
func editSharedProfile(ctx context.Context, id string, sharedEdit func(record) error, profileEdit func(record, record) error) error {
	resolved, err := repo.ResolveProfileID(ctx, id)
	if err != nil {
		return err
	}
	_, _, err = repo.UpdateSharedAndConfigTransaction(ctx, func(tx context.Context, value *jsonx.Object) (*jsonx.Object, error) {
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
	return repo.NewInUse(fmt.Sprintf(msgLibraryUsed, index), []any{jsonx.Obj("kind", "rule", "code", "ruleset_already_used", "id", id, "index", index)})
}
