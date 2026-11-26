package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"unicode/utf8"
)

// A document is stored as one row per (scope, owner, field): the root keys
// besides the sections, the fields of "system" and "shared", and the fields
// of each profile. Order is data, so section and field positions and the
// top-level key order are stored too.

const profilesKey = "profiles"

func isSectionKey(key string) bool { return key == "system" || key == "shared" }

type sectionKey struct{ scope, owner string }

type fieldKey struct{ scope, owner, field string }

// fieldRow is the stored form of one field: its position in the section
// and its value as compact JSON text.
type fieldRow struct {
	position int64
	value    string
}

// ordered is a map that iterates in insertion order (a Python dict).
type ordered[K comparable, V any] struct {
	keys   []K
	values map[K]V
}

func newOrdered[K comparable, V any]() *ordered[K, V] {
	return &ordered[K, V]{values: map[K]V{}}
}

func (o *ordered[K, V]) set(key K, value V) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *ordered[K, V]) get(key K) (V, bool) {
	value, ok := o.values[key]
	return value, ok
}

func (o *ordered[K, V]) has(key K) bool {
	_, ok := o.values[key]
	return ok
}

// rows is a document split by documentRows.
type rows struct {
	rootOrder []string
	sections  *ordered[sectionKey, int64]
	fields    *ordered[fieldKey, fieldRow]
}

// documentRows validates the row envelope and copies the opaque field bytes.
func documentRows(document *Document) (*rows, error) {
	if document == nil {
		return nil, newError(ErrDocument, msgDocumentObject)
	}
	for _, key := range document.Order {
		if !utf8.ValidString(key) {
			return nil, newError(ErrDocument, msgInvalidRowText)
		}
	}
	result := &rows{rootOrder: append([]string{}, document.Order...), sections: newOrdered[sectionKey, int64](), fields: newOrdered[fieldKey, fieldRow]()}
	result.sections.set(sectionKey{"root", ""}, 0)
	add := func(scope, owner string, fields []Field) error {
		for i, field := range fields {
			if !utf8.ValidString(field.Name) {
				return newError(ErrDocument, msgInvalidRowText)
			}
			key := fieldKey{scope, owner, field.Name}
			if result.fields.has(key) {
				return newError(ErrDocument, fmt.Sprintf(msgDuplicateField, scope, owner, field.Name))
			}
			if !utf8.Valid(field.Value) || !json.Valid(field.Value) {
				return newError(ErrDocument, fmt.Sprintf(msgInvalidField, scope, owner, field.Name))
			}
			result.fields.set(key, fieldRow{int64(i), string(field.Value)})
		}
		return nil
	}
	if err := add("root", "", document.Root); err != nil {
		return nil, err
	}
	for _, section := range []struct {
		scope string
		value *Section
	}{{"system", document.System}, {"shared", document.Shared}} {
		if section.value != nil {
			result.sections.set(sectionKey{section.scope, ""}, 0)
			if err := add(section.scope, "", section.value.Fields); err != nil {
				return nil, err
			}
		}
	}
	for i, profile := range document.Profiles {
		if !utf8.ValidString(profile.ID) {
			return nil, newError(ErrDocument, msgInvalidRowText)
		}
		if profile.ID == "" {
			return nil, newError(ErrDocument, msgProfileID)
		}
		key := sectionKey{"profile", profile.ID}
		if result.sections.has(key) {
			return nil, newError(ErrDocument, fmt.Sprintf(msgDuplicateProfile, profile.ID))
		}
		result.sections.set(key, int64(i))
		if err := add("profile", profile.ID, profile.Fields); err != nil {
			return nil, err
		}
	}
	// The same structural invariants apply on reads and writes.
	if _, err := buildDocument(result.rootOrder, result.sections, result.fields); err != nil {
		return nil, &Error{Kind: ErrDocument, Message: err.Error(), Cause: err}
	}
	return result, nil
}

// buildDocument assembles the envelope, never decoding a field's value.
func buildDocument(rootOrder []string, sections *ordered[sectionKey, int64], fields *ordered[fieldKey, fieldRow]) (*Document, error) {
	type item struct {
		position int64
		field    Field
	}
	grouped := map[sectionKey][]item{}
	for _, key := range fields.keys {
		owner := sectionKey{key.scope, key.owner}
		if !sections.has(owner) {
			return nil, newError(ErrCorrupt, fmt.Sprintf(msgOrphanField, key.scope, key.owner, key.field))
		}
		field := fields.values[key]
		grouped[owner] = append(grouped[owner], item{field.position, Field{key.field, json.RawMessage(field.value)}})
	}
	section := func(key sectionKey) []Field {
		items := grouped[key]
		sort.SliceStable(items, func(i, j int) bool { return items[i].position < items[j].position })
		result := make([]Field, 0, len(items))
		for _, item := range items {
			result = append(result, item.field)
		}
		return result
	}
	root := section(sectionKey{"root", ""})
	seen := map[string]bool{}
	expected := 0
	rootNames := map[string]bool{}
	for _, field := range root {
		rootNames[field.Name] = true
	}
	for _, key := range rootOrder {
		if seen[key] {
			return nil, newError(ErrCorrupt, msgRootOrderMismatch)
		}
		seen[key] = true
		if !isSectionKey(key) && key != profilesKey {
			expected++
			if !rootNames[key] {
				return nil, newError(ErrCorrupt, msgRootOrderMismatch)
			}
		}
	}
	if expected != len(root) {
		return nil, newError(ErrCorrupt, msgRootOrderMismatch)
	}
	doc := &Document{Order: slices.Clone(rootOrder), Root: root}
	for _, scope := range []string{"system", "shared"} {
		if seen[scope] != sections.has(sectionKey{scope, ""}) {
			return nil, newError(ErrCorrupt, fmt.Sprintf(msgSectionOrderMismatch, scope))
		}
		if seen[scope] {
			value := &Section{section(sectionKey{scope, ""})}
			if scope == "system" {
				doc.System = value
			} else {
				doc.Shared = value
			}
		}
	}
	profileKeys := []sectionKey{}
	for _, key := range sections.keys {
		if key.scope == "profile" {
			profileKeys = append(profileKeys, key)
		}
	}
	sort.Slice(profileKeys, func(i, j int) bool {
		a, b := profileKeys[i], profileKeys[j]
		if sections.values[a] != sections.values[b] {
			return sections.values[a] < sections.values[b]
		}
		return a.owner < b.owner
	})
	if len(profileKeys) > 0 && !seen[profilesKey] {
		return nil, newError(ErrCorrupt, msgProfilesOrderMismatch)
	}
	for _, key := range profileKeys {
		doc.Profiles = append(doc.Profiles, ProfileRows{key.owner, section(key)})
	}
	return doc, nil
}

// changes is what a commit has to write.
type changes struct {
	deleteSections []sectionKey
	upsertSections []sectionKey
	deleteFields   []fieldKey
	upsertFields   []fieldKey
	rootOrder      bool // the top-level key order differs
}

func (c *changes) any() bool {
	return len(c.deleteSections)+len(c.upsertSections)+len(c.deleteFields)+len(c.upsertFields) > 0 || c.rootOrder
}

func diff(storedRootOrder []string, storedSections *ordered[sectionKey, int64], storedFields *ordered[fieldKey, fieldRow], target *rows) *changes {
	result := &changes{}
	for _, key := range storedSections.keys {
		if !target.sections.has(key) {
			result.deleteSections = append(result.deleteSections, key)
		}
	}
	for _, key := range target.sections.keys {
		if position, ok := storedSections.get(key); !ok || position != target.sections.values[key] {
			result.upsertSections = append(result.upsertSections, key)
		}
	}
	for _, key := range storedFields.keys {
		// Fields of a deleted section go with it (ON DELETE CASCADE).
		if !target.fields.has(key) && target.sections.has(sectionKey{key.scope, key.owner}) {
			result.deleteFields = append(result.deleteFields, key)
		}
	}
	for _, key := range target.fields.keys {
		if row, ok := storedFields.get(key); !ok || row != target.fields.values[key] {
			result.upsertFields = append(result.upsertFields, key)
		}
	}
	result.rootOrder = !slices.Equal(target.rootOrder, storedRootOrder)
	return result
}
