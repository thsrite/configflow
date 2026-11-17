package repo

import (
	"bytes"
	"encoding/json"
	"fmt"

	"configflow/internal/model"
	"configflow/internal/store"
)

// Stored row envelopes belong to repo; model knows records, not SQLite/store.
// A row the model refuses (duplicate member names, D1) is located like the
// store's own corrupt-row diagnostics: scope/owner/field, never its value.
func modelFields(scope, owner string, fields []store.Field) (model.Fields, error) {
	var result model.Fields
	for _, field := range fields {
		if result.Has(field.Name) {
			return model.Fields{}, fmt.Errorf("%w: duplicate stored field", model.ErrJSON)
		}
		if err := result.Set(field.Name, field.Value); err != nil {
			return model.Fields{}, fmt.Errorf(msgModelRowInvalid, scope, owner, field.Name, err)
		}
	}
	return result, nil
}

func decodeModelRows(raw *store.Document) (*model.Document, []model.FieldIssue, error) {
	if raw == nil {
		return nil, nil, fmt.Errorf("decode stored model: missing document")
	}
	roots, err := modelFields("root", "", raw.Root)
	if err != nil {
		return nil, nil, err
	}
	var root model.Fields
	for _, name := range raw.Order {
		var value []byte
		switch name {
		case "system", "shared":
			section := raw.System
			if name == "shared" {
				section = raw.Shared
			}
			if section == nil {
				return nil, nil, fmt.Errorf("decode stored model: missing section")
			}
			fields, err := modelFields(name, "", section.Fields)
			if err != nil {
				return nil, nil, err
			}
			value, err = fields.MarshalJSON()
			if err != nil {
				return nil, nil, err
			}
		case "profiles":
			var profiles model.Fields
			for _, profile := range raw.Profiles {
				if profiles.Has(profile.ID) {
					return nil, nil, fmt.Errorf("%w: duplicate profile owner", model.ErrJSON)
				}
				fields, err := modelFields("profile", profile.ID, profile.Fields)
				if err != nil {
					return nil, nil, err
				}
				encoded, err := fields.MarshalJSON()
				if err != nil {
					return nil, nil, err
				}
				if err := profiles.Set(profile.ID, encoded); err != nil {
					return nil, nil, err
				}
			}
			value, err = profiles.MarshalJSON()
			if err != nil {
				return nil, nil, err
			}
		default:
			var present bool
			value, present = roots.Get(name)
			if !present {
				return nil, nil, fmt.Errorf("decode stored model: missing root field")
			}
		}
		if root.Has(name) {
			return nil, nil, fmt.Errorf("%w: duplicate root member", model.ErrJSON)
		}
		if err := root.Set(name, value); err != nil {
			return nil, nil, err
		}
	}
	encoded, err := root.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	return model.DecodeDocument(encoded)
}

type modelRowKey struct{ scope, owner, name string }

func encodeModelRows(document *model.Document, base *store.Document) (*store.Document, error) {
	if document == nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	encoded, err := model.Marshal(document)
	if err != nil {
		return nil, err
	}
	var root model.Fields
	if err := root.UnmarshalJSON(encoded); err != nil {
		return nil, err
	}
	previous := map[modelRowKey]json.RawMessage{}
	keep := func(scope, owner string, fields []store.Field) {
		for _, field := range fields {
			previous[modelRowKey{scope, owner, field.Name}] = field.Value
		}
	}
	if base != nil {
		keep("root", "", base.Root)
		if base.System != nil {
			keep("system", "", base.System.Fields)
		}
		if base.Shared != nil {
			keep("shared", "", base.Shared.Fields)
		}
		for _, profile := range base.Profiles {
			keep("profile", profile.ID, profile.Fields)
		}
	}
	rows := func(data []byte, scope, owner string) ([]store.Field, error) {
		var fields model.Fields
		if err := fields.UnmarshalJSON(data); err != nil {
			return nil, err
		}
		result := make([]store.Field, 0, fields.Len())
		for name, value := range fields.All() {
			// A JSON member's lexical value excludes separator whitespace. SQLite
			// stores that value independently, including any surrounding whitespace.
			// Retain the complete original row when its value token is unchanged.
			if original, ok := previous[modelRowKey{scope, owner, name}]; ok && bytes.Equal(bytes.TrimSpace(original), bytes.TrimSpace(value)) {
				value = bytes.Clone(original)
			}
			result = append(result, store.Field{Name: name, Value: value})
		}
		return result, nil
	}
	result := &store.Document{Order: root.Keys()}
	for name, value := range root.All() {
		switch name {
		case "system", "shared":
			fields, err := rows(value, name, "")
			if err != nil {
				return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreSectionObject, name), Cause: err}
			}
			if name == "system" {
				result.System = &store.Section{Fields: fields}
			} else {
				result.Shared = &store.Section{Fields: fields}
			}
		case "profiles":
			var profiles model.Fields
			if err := profiles.UnmarshalJSON(value); err != nil {
				return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreProfilesObject, Cause: err}
			}
			for owner, profile := range profiles.All() {
				fields, err := rows(profile, "profile", owner)
				if err != nil {
					return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreProfileObject, owner), Cause: err}
				}
				result.Profiles = append(result.Profiles, store.ProfileRows{ID: owner, Fields: fields})
			}
		default:
			if original, ok := previous[modelRowKey{"root", "", name}]; ok && bytes.Equal(bytes.TrimSpace(original), bytes.TrimSpace(value)) {
				value = bytes.Clone(original)
			}
			result.Root = append(result.Root, store.Field{Name: name, Value: value})
		}
	}
	return result, nil
}

// reportModelIssues runs only at entry publication/load, never on every cache
// hit. A bounded last-head marker also suppresses repeated warnings for an
// uncached oversized document at the same revision. cacheMu owns both states.
func (r *Repository) reportModelIssues(entry storedEntry) {
	r.cacheMu.Lock()
	if r.modelIssueHeadSet && r.modelIssueHead == entry.Head {
		r.cacheMu.Unlock()
		return
	}
	r.modelIssueHeadSet = true
	r.modelIssueHead = entry.Head
	r.cacheMu.Unlock()
	for _, issue := range entry.Issues {
		logger.Warningf(msgModelFieldIssue, issue.Path, issue.Kind)
	}
}
