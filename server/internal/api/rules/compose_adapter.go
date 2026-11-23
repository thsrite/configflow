package rules

// Shared source preparation uses native source and repository owners.
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
)

// libraryWire is a library item without the stored record's lenient codec.
type libraryWire model.RuleLibraryItem

// libraryInput is a new shared library source sent with a composed rule. The
// members prepareLibrary validates, normalizes or drops are kept as sent.
type libraryInput struct {
	libraryWire
	ID         member[string] `json:"id"`
	Name       member[string] `json:"name"`
	Enabled    member[bool]   `json:"enabled"`
	SourceType member[string] `json:"source_type"`
	Behavior   member[string] `json:"behavior"`
	Format     member[string] `json:"format"`
	Content    member[string] `json:"content"`
	URL        member[string] `json:"url"`
	// item is the prepared library item; err is the first member of a type
	// the library record cannot hold.
	item model.RuleLibraryItem
	err  error
}

func (in *libraryInput) remote() bool { return in.SourceType.Or("url") == "url" }

// prepareLibrary validates a new library source; the server assigns its ID.
func prepareLibrary(raw json.RawMessage) (*libraryInput, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, invalid(msgLibrarySource)
	}
	in := &libraryInput{}
	in.err = jsondecode.Unmarshal(raw, in)
	name := strings.TrimSpace(in.Name.Value)
	if !in.Name.OK || name == "" {
		return nil, invalid(msgLibraryName)
	}
	declaration := rulefetch.Declaration{SourceType: in.SourceType, Behavior: in.Behavior, Format: in.Format, Content: in.Content}
	if err := rulefetch.ValidateDeclaration(declaration, nil); err != nil {
		return nil, err
	}
	item := model.RuleLibraryItem(in.libraryWire)
	item.ID, item.Name, item.Enabled = "", name, new(notFalse(in.Enabled))
	optional := func(sent member[string], target **string, nullValue *string) {
		switch {
		case !sent.Present():
		case sent.OK:
			*target = new(sent.Value)
		case sent.Null():
			*target = nullValue
		case in.err == nil:
			in.err = errMemberType
		}
	}
	optional(in.SourceType, &item.SourceType, nil)
	// The stored record reads a null behavior as the empty behavior.
	optional(in.Behavior, &item.Behavior, new(""))
	optional(in.Format, &item.Format, nil)
	if in.remote() {
		item.Content = nil
		optional(in.URL, &item.URL, nil)
	} else {
		item.URL = nil
		optional(in.Content, &item.Content, nil)
	}
	in.item = item
	return in, nil
}

// libraryTypeError reports a library member the library record cannot hold,
// with the message the repository gives for the resulting library list.
func libraryTypeError(entries []model.RuleLibraryItem, cause error) error {
	raw, err := model.Marshal(entries)
	if err != nil {
		return err
	}
	return repo.InputError(&model.MemberTypeError{Record: "shared", Member: "rule_library", Value: raw, Err: cause})
}

func (s *Service) prepareRemote(ctx context.Context, library model.RuleLibraryItem) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := rulefetch.ReadSource(ctx, shared.LibraryRule(library), s.client)
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
func (s *Service) cacheComposed(ctx context.Context, library model.RuleLibraryItem, prepared []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value := shared.LibraryRule(library)
	var err error
	if prepared != nil {
		_, err = rulefetch.WriteCache(ctx, value, rulefetch.Content{Data: prepared}, nil)
	} else {
		_, err = rulefetch.SaveSource(ctx, value, s.client)
	}
	return err
}

// editSharedProfile edits the shared catalog and the profile resolved against
// it in one transaction; profileEdit gets the DNS view from before the edit.
func editSharedProfile(ctx context.Context, id string, sharedEdit func(*model.Shared) error, profileEdit func(*oxidns.Profile, *model.Config) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := repo.ResolveProfileID(ctx, id)
	if err != nil {
		return err
	}
	return repo.UpdateSharedConfigs(ctx, func(tx context.Context, value *model.Shared) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if err := sharedEdit(value); err != nil {
			return err
		}
		return tx.Err()
	}, func(tx context.Context, _ string, before, after *model.Config) error {
		if err := tx.Err(); err != nil {
			return err
		}
		view, err := profileView(before)
		if err != nil {
			return err
		}
		if err := profileEdit(view, after); err != nil {
			return err
		}
		return tx.Err()
	}, func(*model.Shared, []*model.Profile) ([]string, error) { return []string{resolved}, nil })
}
func libraryInUse(id string, index int) error {
	return repo.NewInUse(fmt.Sprintf(msgLibraryUsed, index), []any{struct {
		Kind  string `json:"kind"`
		Code  string `json:"code"`
		ID    string `json:"id"`
		Index int    `json:"index"`
	}{"rule", "ruleset_already_used", id, index}})
}
