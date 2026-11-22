// Package modeltest normalizes stored configuration documents for
// cross-implementation comparisons in tests. It does not decide what the
// repository stores.
package modeltest

import (
	"encoding/json"
	"reflect"
	"strings"

	"configflow/internal/model"
)

// ThroughModel returns a decoded configuration document (encoding/json into
// any, with members system, shared and profiles) as the plain records hold it:
// each section is read with the stored-data rules and written again. Python
// and Go documents compared this way differ only in declared values; members
// no record declares and empty optional members (O18) are left out on both
// sides. A value that is not such a document is returned unchanged.
func ThroughModel(document any) any {
	sections, ok := document.(map[string]any)
	if !ok {
		return document
	}
	out := make(map[string]any, len(sections))
	for key, value := range sections {
		out[key] = value
	}
	if system, ok := sections["system"]; ok {
		out["system"] = through(system, &model.System{})
	}
	if shared, ok := sections["shared"]; ok {
		out["shared"] = through(shared, &model.Shared{})
	}
	if profiles, ok := sections["profiles"].(map[string]any); ok {
		normalized := make(map[string]any, len(profiles))
		for key, profile := range profiles {
			normalized[key] = through(profile, &model.Profile{})
		}
		out["profiles"] = normalized
	}
	return out
}

func through[T any](value any, record *T) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	if _, err := model.DecodeStored(raw, record, ""); err != nil {
		return value
	}
	encoded, err := model.Marshal(record)
	if err != nil {
		return value
	}
	var result any
	if json.Unmarshal(encoded, &result) != nil {
		return value
	}
	return result
}

// RowThroughModel returns one stored row value (decoded JSON) as its record
// member holds it: scope is "system", "shared" or "profile", field the
// member name. Rows no record declares are returned unchanged.
func RowThroughModel(scope, field string, value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var record any
	switch scope {
	case "system":
		record = &model.System{}
	case "shared":
		record = &model.Shared{}
	case "profile":
		record = &model.Profile{}
	default:
		return value
	}
	known, _, err := model.DecodeStoredMember(record, field, raw, field)
	if err != nil || !known {
		return value
	}
	encoded, present, err := model.EncodeMember(record, field)
	if err != nil || !present {
		return nil
	}
	var result any
	if json.Unmarshal(encoded, &result) != nil {
		return value
	}
	return result
}

// AlignResponse returns python without the object members that a Go response
// leaves out because of the plain records (O18 T1c): members no record
// declares (they are dropped on input) and optional members holding "" or
// null. Members are compared by position against goValue; anything else is
// kept, so every other difference still shows.
func AlignResponse(python, goValue any) any { return align(python, goValue, declared()) }

// AlignResponseFor is AlignResponse with the members of the given records
// only (for responses made of those records).
func AlignResponseFor(python, goValue any, records ...any) any {
	names := map[string]bool{}
	for _, record := range records {
		for _, name := range declaredMemberNames(record) {
			names[name] = true
		}
	}
	return align(python, goValue, names)
}

func align(python, goValue any, names map[string]bool) any {
	switch p := python.(type) {
	case map[string]any:
		g, _ := goValue.(map[string]any)
		out := make(map[string]any, len(p))
		for key, item := range p {
			other, present := g[key]
			if !present && (item == nil || item == "" || !names[key]) {
				continue
			}
			out[key] = align(item, other, names)
		}
		return out
	case []any:
		g, _ := goValue.([]any)
		out := make([]any, len(p))
		for i, item := range p {
			var other any
			if i < len(g) {
				other = g[i]
			}
			out[i] = align(item, other, names)
		}
		return out
	}
	return python
}

var declaredNames map[string]bool

func declared() map[string]bool {
	if declaredNames == nil {
		names := map[string]bool{}
		for _, record := range []any{model.Profile{}, model.Shared{}, model.System{}, model.ProxyGroup{}, model.RuleConfig{},
			model.RuleLibraryItem{}, model.Node{}, model.Subscription{}, model.Aggregation{}, model.Router{}, model.SystemConfig{},
			model.Backup{}, model.MihomoSettings{}, model.MosDNSSettings{}, model.DomainDiscovery{}, model.ProviderHealthCheck{}} {
			for _, name := range declaredMemberNames(record) {
				names[name] = true
			}
		}
		declaredNames = names
	}
	return declaredNames
}

func declaredMemberNames(record any) []string {
	var names []string
	typ := reflect.TypeOf(record)
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

// StateThroughModel normalizes a difftest state snapshot ({"document": ...,
// "fields": [[scope, owner, field, position, value]...], ...}) through the
// plain records: the document with ThroughModel, each row with
// RowThroughModel (positions are not compared; a row the record leaves out
// is dropped). Empty optional members are left out everywhere.
func StateThroughModel(state any) any {
	m, ok := state.(map[string]any)
	if !ok {
		return state
	}
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	if document, ok := m["document"]; ok {
		out["document"] = withoutEmpty(ThroughModel(document))
	}
	// Data files written from plain records (subscription caches) leave out
	// empty optional members too.
	if files, ok := m["files"]; ok {
		out["files"] = withoutEmpty(files)
	}
	if rows, ok := m["fields"].([]any); ok {
		normalized := make([]any, 0, len(rows))
		for _, row := range rows {
			if cells, ok := row.([]any); ok && len(cells) == 5 {
				scope, _ := cells[0].(string)
				field, _ := cells[2].(string)
				value := withoutEmpty(RowThroughModel(scope, field, cells[4]))
				if value == nil {
					continue
				}
				row = []any{cells[0], cells[1], cells[2], value}
			}
			normalized = append(normalized, row)
		}
		out["fields"] = normalized
	}
	return out
}

// withoutEmpty drops object members holding "", null, {} or [] at any depth.
func withoutEmpty(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			item = withoutEmpty(item)
			switch x := item.(type) {
			case nil:
				continue
			case string:
				if x == "" {
					continue
				}
			case map[string]any:
				if len(x) == 0 {
					continue
				}
			case []any:
				if len(x) == 0 {
					continue
				}
			}
			out[key] = item
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = withoutEmpty(item)
		}
		return out
	}
	return value
}
