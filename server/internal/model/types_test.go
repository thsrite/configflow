package model

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// Each nested record is exercised independently, so a parent retaining its
// unchanged source cannot mask a child codec dropping members.
func storedRecordTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[ResourceRef](), reflect.TypeFor[ProviderHealthCheck](),
		reflect.TypeFor[Node](), reflect.TypeFor[Subscription](),
		reflect.TypeFor[Aggregation](), reflect.TypeFor[RuleLibraryItem](),
		reflect.TypeFor[MihomoSettings](),
		reflect.TypeFor[SurgeSettings](),
		reflect.TypeFor[SurgeSmartGroup](),
		reflect.TypeFor[LoonSettings](),
		reflect.TypeFor[EasyTierSettings](),
		reflect.TypeFor[EasyTierKeepalive](),
		reflect.TypeFor[EasyTierOptions](),
		reflect.TypeFor[MosDNSSettings](),
		reflect.TypeFor[MosDNSMatch](),
		reflect.TypeFor[DomainDiscovery](),
		reflect.TypeFor[DiscoveryHistory](),
		reflect.TypeFor[DiscoveryRecheck](),
		reflect.TypeFor[ClientFixedIP](),
		reflect.TypeFor[ProxyChain](), reflect.TypeFor[ProxyGroup](), reflect.TypeFor[RuleConfig](),
	}
}

func typeJSON(typ reflect.Type, changed bool) []byte {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ {
	case reflect.TypeFor[TextLines]():
		if changed {
			return []byte(`["qname edited.example.com"]`)
		}
		return []byte(`"qname example.com\n!qtype 28"`)
	case reflect.TypeFor[Dynamic]():
		return []byte(`{ "engine-extension": [1.0,"\u4f8b",true] }`)
	case reflect.TypeFor[Int]():
		if changed {
			return []byte(`61`)
		}
		return []byte(`6e1`)
	}
	switch typ.Kind() {
	case reflect.String:
		if changed {
			return []byte(`"edited <>& 例"`)
		}
		return []byte(`"\u4f8b <>&"`)
	case reflect.Bool:
		if changed {
			return []byte(`false`)
		}
		return []byte(`true`)
	case reflect.Float64:
		if changed {
			return []byte(`2.5`)
		}
		return []byte(`1.25`)
	case reflect.Slice:
		if changed {
			return []byte(`[]`)
		}
		return append(append([]byte(`[`), typeJSON(typ.Elem(), false)...), ']')
	case reflect.Struct:
		if container, ok := reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type }); ok {
			return append(append([]byte(`{ "synthetic.example.com": `), typeJSON(container.storedValueType(), changed)...), '}')
		}
		schema := schemaFor(typ)
		if schema.err != nil {
			panic("invalid test schema")
		}
		out := []byte(` { "future": { "z":1.0, "a":"\u4f8b <>&" }`)
		for _, field := range schema.fields {
			key, _ := json.Marshal(field.name)
			out = append(out, ',')
			out = append(out, key...)
			out = append(out, ':')
			out = append(out, typeJSON(typ.Field(field.index).Type, changed)...)
		}
		return append(out, []byte(` } `)...)
	}
	panic("missing synthetic fixture type")
}

func TestStoredTypes(t *testing.T) {
	for _, typ := range storedRecordTypes() {
		t.Run(typ.Name(), func(t *testing.T) {
			source := typeJSON(typ, false)
			decode := func(data []byte) reflect.Value {
				t.Helper()
				rec := reflect.New(typ)
				if rec.Interface().(json.Unmarshaler).UnmarshalJSON(data) != nil {
					t.Fatal("record decode failed")
				}
				return rec
			}
			rec := decode(source)
			encoded, err := Marshal(rec.Interface())
			if err != nil || !bytes.Equal(source, encoded) {
				t.Fatal("no-op changed bytes")
			}
			if issues := valueIssues(rec, ""); len(issues) != 0 {
				t.Fatalf("valid fields rejected: %v", issues)
			}
			order, before := fuzzMembers(t, source)
			for _, field := range schemaFor(typ).fields {
				t.Run(field.name, func(t *testing.T) {
					current := decode(source)
					next := typeJSON(typ.Field(field.index).Type, true)
					target := current.Elem().Field(field.index)
					if json.Unmarshal(next, target.Addr().Interface()) != nil {
						t.Fatal("edit decode failed")
					}
					result, err := Marshal(current.Interface())
					if err != nil {
						t.Fatal("edit encode failed")
					}
					afterOrder, after := fuzzMembers(t, result)
					if !slices.Equal(order, afterOrder) {
						t.Fatal("edit moved members")
					}
					for name, original := range before {
						if name != field.name && !bytes.Equal(original, after[name]) {
							t.Fatal("edit changed unrelated member")
						}
					}
					// Wrong container/scalar types must produce a value-free R5 issue and
					// preserve the entire invalid fragment even across an unrelated edit.
					wrong := []byte(`{"secret":"synthetic-never-log"}`)
					base := typ.Field(field.index).Type
					for base.Kind() == reflect.Pointer {
						base = base.Elem()
					}
					if base.Kind() == reflect.Struct || base == reflect.TypeFor[Dynamic]() {
						wrong = []byte(`false`)
					}
					if base == reflect.TypeFor[Dynamic]() {
						return
					} // all JSON shapes are valid by definition
					corrupt := bytes.Replace(source, append([]byte(`"`+field.name+`":`), typeJSON(typ.Field(field.index).Type, false)...), append([]byte(`"`+field.name+`":`), wrong...), 1)
					bad := decode(corrupt)
					issues := valueIssues(bad, "")
					if !slices.Contains(issues, FieldIssue{field.name, IssueType}) {
						t.Fatal("missing R5 diagnostic")
					}
					untouched, err := Marshal(bad.Interface())
					if err != nil || !bytes.Equal(untouched, corrupt) {
						t.Fatal("R5 no-op changed bytes")
					}
					fieldsIndex := schemaFor(typ).extra
					extra := bad.Elem().Field(fieldsIndex).Addr().Interface().(*Fields)
					if extra.Set("another_future", json.RawMessage(`true`)) != nil {
						t.Fatal("unknown edit failed")
					}
					preserved, err := Marshal(bad.Interface())
					if err != nil {
						t.Fatal("R5 edit encode failed")
					}
					_, members := fuzzMembers(t, preserved)
					if !bytes.Equal(members[field.name], wrong) {
						t.Fatal("R5 lost original fragment")
					}
					if target.Kind() == reflect.Pointer {
						clean := decode(source)
						clean.Elem().Field(field.index).SetZero()
						deleted, err := Marshal(clean.Interface())
						if err != nil {
							t.Fatal("pointer delete failed")
						}
						_, members := fuzzMembers(t, deleted)
						if _, ok := members[field.name]; ok {
							t.Fatal("nil pointer did not remove member")
						}
					}
				})
			}
		})
	}
}

type interchangeRow struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

func pythonTypes(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command(os.Getenv("CONFIGFLOW_PY"), append([]string{"testdata/types_interchange.py"}, args...)...)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if err := cmd.Run(); err != nil {
		t.Fatal("Python types boundary failed (output suppressed)")
	}
}

func TestTypesPythonRoundTrip(t *testing.T) {
	if os.Getenv("CONFIGFLOW_PY") == "" {
		t.Skip("set CONFIGFLOW_PY for real Python interchange")
	}
	var rows []interchangeRow
	for _, typ := range storedRecordTypes() {
		rows = append(rows, interchangeRow{typ.Name(), typeJSON(typ, false)})
	}
	runTypesInterchange(t, rows, false)
}

func TestTypesPrivateSnapshot(t *testing.T) {
	source := os.Getenv("CONFIGFLOW_SNAPSHOT")
	if source == "" || os.Getenv("CONFIGFLOW_PY") == "" {
		t.Skip("set CONFIGFLOW_SNAPSHOT and CONFIGFLOW_PY")
	}
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory permissions failed")
	}
	path := filepath.Join(dir, "rows.json")
	pythonTypes(t, "snapshot", filepath.Join(source, "configflow.sqlite3"), path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private rows read failed")
	}
	var rows []interchangeRow
	if json.Unmarshal(data, &rows) != nil {
		t.Fatal("private rows decode failed")
	}
	checkSnapshotTypes(t, rows)
	runTypesInterchange(t, rows, true)
}

func runTypesInterchange(t *testing.T, rows []interchangeRow, private bool) {
	t.Helper()
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory permissions failed")
	}
	input, prepared, output := filepath.Join(dir, "input.json"), filepath.Join(dir, "python.json"), filepath.Join(dir, "go.json")
	data, err := json.Marshal(rows)
	if err != nil || os.WriteFile(input, data, 0600) != nil {
		t.Fatal("boundary input failed")
	}
	pythonTypes(t, "write", input, prepared)
	data, err = os.ReadFile(prepared)
	if err != nil || json.Unmarshal(data, &rows) != nil {
		t.Fatal("Python-written input failed")
	}
	registry := map[string]reflect.Type{}
	for _, typ := range storedRecordTypes() {
		registry[typ.Name()] = typ
	}
	rowTypes := map[string]reflect.Type{
		"mihomo":           reflect.TypeFor[Profile](),
		"surge":            reflect.TypeFor[Profile](),
		"loon":             reflect.TypeFor[Profile](),
		"mosdns":           reflect.TypeFor[Profile](),
		"domain_discovery": reflect.TypeFor[Profile](),
		"device_notes":     reflect.TypeFor[Profile](),
		"client_fixed_ips": reflect.TypeFor[Profile](),
		"oxidns":           reflect.TypeFor[Profile](),
		"proxy_groups":     reflect.TypeFor[Profile](), "rule_configs": reflect.TypeFor[Profile](),
		"nodes": reflect.TypeFor[Shared](), "subscriptions": reflect.TypeFor[Shared](),
		"subscription_aggregations": reflect.TypeFor[Shared](), "rule_library": reflect.TypeFor[Shared](),
	}
	counts := map[string]int{}
	for i := range rows {
		typ, ok := registry[rows[i].Name]
		if !ok {
			typ, ok = rowTypes[rows[i].Name]
		}
		if !ok {
			continue
		}
		value := reflect.New(typ)
		source := rows[i].Value
		_, wrapped := rowTypes[rows[i].Name]
		if wrapped {
			key, _ := json.Marshal(rows[i].Name)
			source = append(append(append([]byte("{"), key...), ':'), source...)
			source = append(source, '}')
		}
		if value.Interface().(json.Unmarshaler).UnmarshalJSON(source) != nil {
			t.Fatal("stored type decode failed")
		}
		if issues := valueIssues(value, rows[i].Name); len(issues) != 0 {
			t.Fatalf("stored type issues (schema only): %v", issues)
		}
		encoded, err := Marshal(value.Interface())
		if err == nil && wrapped {
			_, members := fuzzMembers(t, encoded)
			encoded = members[rows[i].Name]
		}
		if err != nil || !bytes.Equal(encoded, rows[i].Value) {
			t.Fatal("stored type no-op changed bytes")
		}
		// DecodeRecord owns its source. Confirm the caller's read-only bytes survive.
		rows[i].Value = encoded
		counts[rows[i].Name]++
	}
	data, err = json.Marshal(rows)
	if err != nil || os.WriteFile(output, data, 0600) != nil {
		t.Fatal("Go boundary output failed")
	}
	pythonTypes(t, "audit", prepared, output)
	if private {
		for name := range rowTypes {
			if counts[name] == 0 {
				t.Fatalf("snapshot lacks row for %s", name)
			}
		}
		t.Logf("private row coverage (counts only): %v", counts)
	}
}

func FuzzStoredTypes(f *testing.F) {
	for i, typ := range storedRecordTypes() {
		f.Add(uint8(i), typeJSON(typ, false))
	}
	f.Fuzz(func(t *testing.T, index uint8, data []byte) {
		types := storedRecordTypes()
		typ := types[int(index)%len(types)]
		value := reflect.New(typ)
		if value.Interface().(json.Unmarshaler).UnmarshalJSON(data) != nil {
			return
		}
		encoded, err := Marshal(value.Interface())
		if err != nil || !bytes.Equal(encoded, data) {
			t.Fatal("type no-op changed bytes")
		}
		_, before := fuzzMembers(t, data)
		schema := schemaFor(typ)
		field := schema.fields[0]
		if json.Unmarshal(typeJSON(typ.Field(field.index).Type, true), value.Elem().Field(field.index).Addr().Interface()) != nil {
			t.Fatal("edit failed")
		}
		changed, err := Marshal(value.Interface())
		if err != nil {
			t.Fatal("encode failed")
		}
		_, after := fuzzMembers(t, changed)
		for name, member := range before {
			if name != field.name && !bytes.Equal(member, after[name]) {
				t.Fatal("unrelated member changed")
			}
		}
	})
}

func TestMosDNSMatchesForms(t *testing.T) {
	for _, source := range []string{
		`{"matches":"qname example.com\n!qtype 28","exec":"return","future": 1.0}`,
		`{"matches":["qname example.com","!qtype 28"],"exec":"return"}`,
		`{"matches":null,"exec":"return"}`,
	} {
		var value MosDNSMatch
		if value.UnmarshalJSON([]byte(source)) != nil {
			t.Fatal("match decode failed")
		}
		encoded, err := Marshal(value)
		if err != nil || !bytes.Equal(encoded, []byte(source)) {
			t.Fatal("match representation changed")
		}
		value.Exec = "reject 3"
		edited, err := Marshal(value)
		if err != nil {
			t.Fatal("match edit failed")
		}
		_, before := fuzzMembers(t, []byte(source))
		_, after := fuzzMembers(t, edited)
		if !bytes.Equal(before["matches"], after["matches"]) {
			t.Fatal("match form lost on edit")
		}
	}
}

// checkSnapshotTypes starts from original SQLite value text, before Python's
// JSON writer normalizes it. Each actual nested record is independently edited
// so its parent's Raw cannot hide a missing type declaration or child codec.
func checkSnapshotTypes(t *testing.T, rows []interchangeRow) {
	t.Helper()
	counts := map[string]int{}
	for _, row := range rows {
		var holder json.Unmarshaler
		switch row.Name {
		case "nodes", "subscriptions", "subscription_aggregations", "rule_library":
			holder = &Shared{}
		case "proxy_groups", "rule_configs", "mihomo", "surge", "loon", "mosdns", "domain_discovery", "device_notes", "client_fixed_ips", "oxidns":
			holder = &Profile{}
		default:
			continue
		}
		key, _ := json.Marshal(row.Name)
		source := append(append(append([]byte("{"), key...), ':'), row.Value...)
		source = append(source, '}')
		if holder.UnmarshalJSON(source) != nil {
			t.Fatal("snapshot wrapper decode failed")
		}
		assertStoredTree(t, source, reflect.ValueOf(holder), counts)
	}
	t.Logf("original SQLite nested record coverage (counts only): %v", counts)
}

func assertStoredTree(t *testing.T, source []byte, value reflect.Value, counts map[string]int) {
	t.Helper()
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(source), []byte("null")) {
		return
	}
	typ := value.Type()
	if typ == reflect.TypeFor[Dynamic]() || typ == reflect.TypeFor[TextLines]() {
		return
	}
	switch value.Kind() {
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(source, &items) != nil || len(items) != value.Len() {
			t.Fatal("snapshot array shape changed")
		}
		for i, item := range items {
			assertStoredTree(t, item, value.Index(i), counts)
		}
	case reflect.Struct:
		if _, ok := reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type }); ok {
			order, members := fuzzMembers(t, source)
			for _, name := range order {
				child := value.MethodByName("Get").Call([]reflect.Value{reflect.ValueOf(name)})[0]
				assertStoredTree(t, members[name], child, counts)
			}
			return
		}
		schema := schemaFor(typ)
		if schema.raw < 0 {
			return
		}
		fresh := reflect.New(typ)
		if fresh.Interface().(json.Unmarshaler).UnmarshalJSON(source) != nil {
			t.Fatal("snapshot nested decode failed")
		}
		output, err := Marshal(fresh.Interface())
		if err != nil || !bytes.Equal(output, source) {
			t.Fatal("snapshot nested no-op changed bytes")
		}
		counts[typ.Name()]++
		_, members := fuzzMembers(t, source)
		for _, field := range schema.fields {
			if original, ok := members[field.name]; ok {
				assertStoredTree(t, original, value.Field(field.index), counts)
			}
		}
		// Editing a present primitive (or appending the first primitive) must keep
		// every other actual member byte-identical, including unknown future keys.
		for _, field := range schema.fields {
			target := fresh.Elem().Field(field.index)
			base := target.Type()
			for base.Kind() == reflect.Pointer {
				base = base.Elem()
			}
			if base.Kind() != reflect.String && base.Kind() != reflect.Bool && base != reflect.TypeFor[Int]() {
				continue
			}
			if json.Unmarshal(typeJSON(target.Type(), true), target.Addr().Interface()) != nil {
				t.Fatal("snapshot nested edit failed")
			}
			edited, err := Marshal(fresh.Interface())
			if err != nil {
				t.Fatal("snapshot nested encode failed")
			}
			_, after := fuzzMembers(t, edited)
			for name, original := range members {
				if name != field.name && !bytes.Equal(original, after[name]) {
					t.Fatal("snapshot edit changed unrelated bytes")
				}
			}
			break
		}
	}
}
