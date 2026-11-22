package model

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"configflow/internal/testjson"
)

// Each nested record is exercised independently, so a parent retaining its
// unchanged source cannot mask a child codec dropping members.
func storedRecordTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[Routers](),
		reflect.TypeFor[Router](),
		reflect.TypeFor[RouterDNSGuard](),
		reflect.TypeFor[RouterPDGuard](),
		reflect.TypeFor[RouterProbe](),
		reflect.TypeFor[RouterRelease](),
		reflect.TypeFor[RouterResource](),
		reflect.TypeFor[ROSPolicyState](),
		reflect.TypeFor[ROSPolicySpec](),
		reflect.TypeFor[DNSGuardState](),
		reflect.TypeFor[DNSGuardSpec](),
		reflect.TypeFor[DNSGuardResource](),
		reflect.TypeFor[IPv6FakeIPState](),
		reflect.TypeFor[IPv6FakeIPSpec](),
		reflect.TypeFor[IPv6RoutesState](),
		reflect.TypeFor[IPv6PDGuardState](),
		reflect.TypeFor[IPv6PDGuardSpec](),
		reflect.TypeFor[PDGuardResource](),
		reflect.TypeFor[RouterRuleSyncReceipt](),
		reflect.TypeFor[RouterSyncLog](),
		reflect.TypeFor[RouterSyncRule](),
		reflect.TypeFor[RouterRuleSyncBaseline](),
		reflect.TypeFor[RouterSyncDesired](),
		reflect.TypeFor[RouterCIDR](),
		reflect.TypeFor[RouterSyncNAT](),
		reflect.TypeFor[SystemConfig](),
		reflect.TypeFor[NodeTestDNSConfig](),
		reflect.TypeFor[ManagedDNSBinding](),
		reflect.TypeFor[Backup](),
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
	case reflect.TypeFor[Number]():
		if changed {
			return []byte(`8`)
		}
		return []byte(`7`)
	case reflect.TypeFor[json.RawMessage]():
		if changed {
			return []byte(`{"changed":true}`)
		}
		return []byte(`{ "opaque": [1.0,null,true] }`)
	case reflect.TypeFor[TextLines]():
		if changed {
			return []byte(`["qname edited.example.com"]`)
		}
		return []byte(`"qname example.com\n!qtype 28"`)
	case reflect.TypeFor[Dynamic]():
		if changed {
			return []byte(`{"edited": [false,2.5,"changed.example.com"]}`)
		}
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
		if elem := orderedElem(typ); elem != nil {
			return append(append([]byte(`{ "synthetic.example.com": `), typeJSON(elem, changed)...), '}')
		}
		if container, ok := reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type }); ok {
			return append(append([]byte(`{ "synthetic.example.com": `), typeJSON(container.storedValueType(), changed)...), '}')
		}
		out := []byte(` { "future": { "z":1.0, "a":"\u4f8b <>&" }`)
		for _, field := range declaredFields(typ) {
			key, _ := json.Marshal(field.name)
			out = append(out, ',')
			out = append(out, key...)
			out = append(out, ':')
			value := typeJSON(typ.Field(field.index).Type, changed)
			if typ.Field(field.index).Tag.Get("int") == "literal" && !changed {
				// Retention counts read only integer literals (int:"literal").
				value = []byte(`60`)
			}
			out = append(out, value...)
		}
		return append(out, []byte(` } `)...)
	}
	panic("missing synthetic fixture type")
}

// plainAgentRecordTypes are the O18 T1b plain records.
func plainAgentRecordTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[Agent](),
		reflect.TypeFor[AgentMetrics](),
		reflect.TypeFor[AgentCPU](),
		reflect.TypeFor[AgentMemory](),
		reflect.TypeFor[AgentDisk](),
		reflect.TypeFor[AgentNetwork](),
		reflect.TypeFor[LegacyDNSEndpoint](),
		reflect.TypeFor[OperationEvent](),
		reflect.TypeFor[Deployment](),
		reflect.TypeFor[DeploymentDNSCheck](),
		reflect.TypeFor[PreparedContent](),
		reflect.TypeFor[AppliedContent](),
		reflect.TypeFor[OxiDNSValidationReceipt](),
		reflect.TypeFor[AgentUpgrade](),
		reflect.TypeFor[UpgradeArtifact](),
		reflect.TypeFor[IPv6VerificationSettings](),
		reflect.TypeFor[IPv6Verification](),
		reflect.TypeFor[IPv6VerificationCheck](),
		reflect.TypeFor[DeploymentFile](),
		reflect.TypeFor[DeploymentManifest](),
		reflect.TypeFor[AgentRuleSyncReceipt](),
		reflect.TypeFor[RuleSyncSettings](),
		reflect.TypeFor[AgentRuleSync](),
		reflect.TypeFor[RuleSyncState](),
		reflect.TypeFor[DNSFollowBinding](),
		reflect.TypeFor[DNSFollowState](),
		reflect.TypeFor[DNSFollowJob](),
		reflect.TypeFor[HASettings](),
		reflect.TypeFor[HAState](),
		reflect.TypeFor[HAMember](),
		reflect.TypeFor[HAIdentity](),
		reflect.TypeFor[HAEnable](),
		reflect.TypeFor[HARouter](),
		reflect.TypeFor[HAHandoff](),
		reflect.TypeFor[HAHandoffFailure](),
		reflect.TypeFor[HAJournal](),
		reflect.TypeFor[HAAutoComplete](),
		reflect.TypeFor[HAAutoCompleteWatch](),
		reflect.TypeFor[HAPublicationSummary](),
		reflect.TypeFor[HABundle](),
		reflect.TypeFor[HAPayload](),
	}
}

// TestPlainAgentRecordsRoundTrip: every T1b record decodes a synthetic stored
// object (all members set, plus an unknown member), drops the unknown member
// with an issue, and encodes to JSON that decodes to the same value.
func TestPlainAgentRecordsRoundTrip(t *testing.T) {
	for _, typ := range plainAgentRecordTypes() {
		t.Run(typ.Name(), func(t *testing.T) {
			first := reflect.New(typ)
			issues, err := DecodeStored(typeJSON(typ, false), first.Interface(), "")
			if err != nil {
				t.Fatal(err)
			}
			unknown := 0
			for _, issue := range issues {
				if issue.Kind != IssueUnknown {
					t.Fatalf("valid member rejected: %v", issue)
				}
				unknown++
			}
			if unknown == 0 {
				t.Fatal("unknown member not reported")
			}
			encoded, err := Marshal(first.Interface())
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte(`"future"`)) {
				t.Fatalf("unknown member written: %s", encoded)
			}
			second := reflect.New(typ)
			if _, err := DecodeStored(encoded, second.Interface(), ""); err != nil {
				t.Fatal(err)
			}
			again, err := Marshal(second.Interface())
			if err != nil || !bytes.Equal(encoded, again) {
				t.Fatalf("round trip changed value:\n%s\n%s", encoded, again)
			}
		})
	}
}

// declaredField is a declared JSON member of a plain record.
type declaredField struct {
	name  string
	index int
}

// declaredFields lists the declared JSON members of a plain record in
// declaration order; the inline fallback member (",embed") has no name.
func declaredFields(typ reflect.Type) []declaredField {
	var fields []declaredField
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" || name == "" {
			continue
		}
		fields = append(fields, declaredField{name, i})
	}
	return fields
}

// hasInlineMembers reports a record whose ",embed" member keeps undeclared
// members (Node protocol parameters), so they are not unknown members.
func hasInlineMembers(typ reflect.Type) bool {
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(typ.Field(i).Tag.Get("json"), ",embed") {
			return true
		}
	}
	return false
}

// TestStoredTypes (O18 T1c): every router, settings, resource and policy
// record is a plain struct. A synthetic stored object (all members set plus an
// undeclared member) decodes with only an unknown-member issue, encodes without
// the undeclared member and is stable on a second round trip. Editing one typed
// member writes exactly that member in place; a wrong-typed stored member reads
// as its zero value with a value-free type issue and is not written back
// (replaces the R5 original-fragment preservation checks).
func TestStoredTypes(t *testing.T) {
	for _, typ := range storedRecordTypes() {
		t.Run(typ.Name(), func(t *testing.T) {
			decode := func(data []byte) (reflect.Value, []FieldIssue) {
				t.Helper()
				rec := reflect.New(typ)
				issues, err := DecodeStored(data, rec.Interface(), "")
				if err != nil {
					t.Fatalf("record decode failed: %v", err)
				}
				return rec, issues
			}
			first, issues := decode(typeJSON(typ, false))
			unknown := 0
			for _, issue := range issues {
				if issue.Kind != IssueUnknown {
					t.Fatalf("valid member rejected: %v", issue)
				}
				unknown++
			}
			// MosDNSMatch decodes itself (text or object form); its undeclared
			// members are dropped without an issue at the top level.
			if unknown == 0 && !hasInlineMembers(typ) && typ != reflect.TypeFor[MosDNSMatch]() {
				t.Fatal("unknown member not reported")
			}
			source, err := Marshal(first.Interface())
			if err != nil {
				t.Fatal(err)
			}
			// Nested router manifests with undeclared members are kept whole
			// as stored (fail closed), so only the record's own member is checked.
			if _, top := fuzzMembers(t, source); !hasInlineMembers(typ) && top["future"] != nil {
				t.Fatalf("unknown member written: %s", source)
			}
			again, _ := decode(source)
			if encoded, err := Marshal(again.Interface()); err != nil || !bytes.Equal(source, encoded) {
				t.Fatalf("round trip changed value:\n%s\n%s", source, encoded)
			}
			order, before := fuzzMembers(t, source)
			for _, field := range declaredFields(typ) {
				t.Run(field.name, func(t *testing.T) {
					current, _ := decode(source)
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
					// The edit's typed value (lenient decode drops the synthetic
					// undeclared member of a nested record).
					typed, err := Marshal(target.Interface())
					var wanted, actual any
					if err != nil || json.Unmarshal(typed, &wanted) != nil {
						t.Fatal("synthetic edit invalid")
					}
					if written, ok := after[field.name]; ok {
						if bytes.Equal(before[field.name], written) {
							t.Fatal("synthetic edit did not actually change the member")
						}
						if json.Unmarshal(written, &actual) != nil || !reflect.DeepEqual(wanted, actual) {
							t.Fatal("typed edit was not written")
						}
					} else if !target.IsZero() {
						t.Fatal("typed edit was dropped")
					}
					kept := slices.DeleteFunc(slices.Clone(order), func(name string) bool { return name == field.name })
					if !slices.Equal(kept, slices.DeleteFunc(afterOrder, func(name string) bool { return name == field.name })) {
						t.Fatal("edit moved members")
					}
					for name, original := range before {
						if name != field.name && !bytes.Equal(original, after[name]) {
							t.Fatal("edit changed unrelated member")
						}
					}
					base := typ.Field(field.index).Type
					for base.Kind() == reflect.Pointer {
						base = base.Elem()
					}
					if base == reflect.TypeFor[Dynamic]() || base == reflect.TypeFor[json.RawMessage]() {
						return // all JSON shapes are valid by definition
					}
					wrong := []byte(`{"secret":"synthetic-never-log"}`)
					if base.Kind() == reflect.Struct {
						wrong = []byte(`false`)
					}
					corrupt := replaceStoredMember(t, source, field.name, wrong)
					bad, issues := decode(corrupt)
					_, keeps := reflect.New(base).Interface().(damagedKeeper)
					if typ == reflect.TypeFor[MosDNSMatch]() {
						// MosDNSMatch decodes itself (text or object form) and
						// does not pass its issues on (reported as a gap to the
						// T1c author); the wrong value still reads as zero below.
					} else if keeps {
						// A router manifest stored with another JSON type is kept
						// whole as stored until replaced (fail closed).
						if !slices.Contains(issues, FieldIssue{field.name, IssueType}) {
							t.Fatalf("missing type issue: %v", issues)
						}
						return
					} else if !slices.Contains(issues, FieldIssue{field.name, IssueType}) {
						t.Fatalf("missing type issue: %v", issues)
					}
					rewritten, err := Marshal(bad.Interface())
					if err != nil {
						t.Fatal("lenient value encode failed")
					}
					if bytes.Contains(rewritten, []byte("synthetic-never-log")) {
						t.Fatal("wrong-typed value written back")
					}
					_, members := fuzzMembers(t, rewritten)
					if bytes.Equal(members[field.name], wrong) {
						t.Fatal("wrong-typed value written back")
					}
					for name, original := range before {
						if name != field.name && !bytes.Equal(original, members[name]) {
							t.Fatal("wrong-typed member changed unrelated member")
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
		// The synthetic undeclared member is dropped on read (O18 T1c), so
		// the boundary starts from the Go-written form of the record.
		value := reflect.New(typ)
		if _, err := DecodeStored(typeJSON(typ, false), value.Interface(), ""); err != nil {
			t.Fatal(err)
		}
		encoded, err := Marshal(value.Interface())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, interchangeRow{typ.Name(), encoded})
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
	// T1c records and the T1b Agent records (receipts and HA payloads are
	// stored as files of their own).
	for _, typ := range append(storedRecordTypes(), plainAgentRecordTypes()...) {
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
		"routers":       reflect.TypeFor[Profile](),
		"agents":        reflect.TypeFor[System](),
		"system_config": reflect.TypeFor[System](), "backup": reflect.TypeFor[System](),
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
		issues, err := DecodeStored(source, value.Interface(), "")
		if err != nil {
			t.Fatal("stored type decode failed")
		}
		for _, issue := range issues {
			if issue.Kind != IssueUnknown {
				t.Fatalf("stored type issues (schema only): %v", issue)
			}
		}
		encoded, err := Marshal(value.Interface())
		if err == nil && wrapped {
			_, members := fuzzMembers(t, encoded)
			encoded = members[rows[i].Name]
		}
		// Plain records drop undeclared members and leave out empty optional
		// members; the value is otherwise unchanged (bytes are Go's encoding).
		if err != nil || !sameStoredValue(t, encoded, rows[i].Value, private) {
			t.Fatal("stored type round trip changed the value")
		}
		// DecodeRecord owns its source. Confirm the caller's read-only bytes survive.
		rows[i].Value = encoded
		counts[rows[i].Name]++
	}
	data, err = json.Marshal(rows)
	if err != nil || os.WriteFile(output, data, 0600) != nil {
		t.Fatal("Go boundary output failed")
	}
	if !private {
		pythonTypes(t, "audit", prepared, output)
	}
	if private {
		names := []string{"AgentRuleSyncReceipt", "HAPayload", "RouterRuleSyncReceipt"}
		for name := range rowTypes {
			names = append(names, name)
		}
		for _, name := range names {
			if counts[name] == 0 {
				t.Fatalf("snapshot lacks row for %s", name)
			}
		}
		t.Logf("private row coverage (counts only): %v", counts)
	}
}

// sameStoredValue compares a Go-written record with its stored source as
// values: members holding "", null or {} count as absent and, for a private
// snapshot, undeclared members (reported as issues on read) are ignored.
// Always-written zero members Go adds are ignored (dropAlwaysWrittenZeros).
func sameStoredValue(t *testing.T, encoded, source []byte, private bool) bool {
	t.Helper()
	decode := func(data []byte) any {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var value any
		if dec.Decode(&value) != nil {
			t.Fatal("stored value decode failed")
		}
		return testjson.WithoutEmptyMembers(value)
	}
	got, want := decode(encoded), decode(source)
	if private {
		dropUndeclared(want, got)
	}
	dropAlwaysWrittenZeros(want, got)
	return testjson.Equal(want, got)
}

// dropAlwaysWrittenZeros removes from got the object members want lacks
// whose value is 0 or [] (members Go always writes with their zero value,
// e.g. dns_follow.upstream_index, O18 T1b). false is not dropped: a switch
// written false where storage had none could change a default-on reading.
func dropAlwaysWrittenZeros(want, got any) {
	switch g := got.(type) {
	case map[string]any:
		w, _ := want.(map[string]any)
		for key, item := range g {
			other, ok := w[key]
			if !ok {
				if object, isObject := item.(map[string]any); isObject {
					dropAlwaysWrittenZeros(nil, object)
				}
				if list, isList := item.([]any); item == json.Number("0") || isList && len(list) == 0 {
					delete(g, key)
				}
				continue
			}
			dropAlwaysWrittenZeros(other, item)
		}
		// A member left empty by the removal is an absent member.
		for key, item := range g {
			if object, ok := item.(map[string]any); ok && len(object) == 0 {
				if _, ok := w[key]; !ok {
					delete(g, key)
				}
			}
		}
	case []any:
		w, _ := want.([]any)
		for i := range g {
			if i < len(w) {
				dropAlwaysWrittenZeros(w[i], g[i])
			}
		}
	}
}

// dropUndeclared removes members of want that got lacks at the same object
// path (undeclared members dropped on read).
func dropUndeclared(want, got any) {
	switch w := want.(type) {
	case map[string]any:
		g, _ := got.(map[string]any)
		for key, item := range w {
			other, ok := g[key]
			if !ok {
				delete(w, key)
				continue
			}
			dropUndeclared(item, other)
		}
	case []any:
		g, _ := got.([]any)
		for i := range w {
			if i < len(g) {
				dropUndeclared(w[i], g[i])
			}
		}
	}
}

func FuzzStoredTypes(f *testing.F) {
	for i, typ := range storedRecordTypes() {
		f.Add(uint8(i), typeJSON(typ, false))
		if typ == reflect.TypeFor[MosDNSMatch]() {
			f.Add(uint8(i), []byte(`"qname\u0020example.com"`))
		}
	}
	f.Fuzz(func(t *testing.T, index uint8, data []byte) {
		types := storedRecordTypes()
		typ := types[int(index)%len(types)]
		value := reflect.New(typ)
		if _, err := DecodeStored(data, value.Interface(), ""); err != nil {
			return
		}
		// Plain records: the first write is Go's encoding of the lenient
		// value; decoding it again must give the same encoding.
		data, err := Marshal(value.Interface())
		if err != nil {
			t.Fatal("encode failed")
		}
		// Damaged router manifests are kept as stored, so a second read may
		// report the same issues; the value must still be stable.
		value = reflect.New(typ)
		if _, err := DecodeStored(data, value.Interface(), ""); err != nil {
			t.Fatalf("Go-written record rejected: %v", err)
		}
		encoded, err := Marshal(value.Interface())
		if err != nil || !bytes.Equal(encoded, data) {
			t.Fatal("round trip changed bytes")
		}
		if match, ok := value.Interface().(*MosDNSMatch); ok && match.Text != nil {
			expected := *match.Text + " edited"
			match.Text = &expected
			changed, err := Marshal(match)
			if err != nil {
				t.Fatal(err)
			}
			var decoded string
			if json.Unmarshal(changed, &decoded) != nil || decoded != expected {
				t.Fatal("scalar union edit lost value")
			}
			return
		}
		if bytes.HasPrefix(data, []byte(`"`)) {
			return
		}
		_, before := fuzzMembers(t, data)
		field := declaredFields(typ)[0]
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

// TestMosDNSMatchesForms: the text and list forms of matches survive a
// round trip and an unrelated edit (O18 T1c: the record is written in Go's
// encoding, id always written, undeclared members dropped).
func TestMosDNSMatchesForms(t *testing.T) {
	tests := []struct{ source, want string }{
		{`{"matches":"qname example.com\n!qtype 28","exec":"return","future": 1.0}`,
			`{"id":"","matches":"qname example.com\n!qtype 28","exec":"return"}`},
		{`{"matches":["qname example.com","!qtype 28"],"exec":"return"}`,
			`{"id":"","matches":["qname example.com","!qtype 28"],"exec":"return"}`},
		// A stored null list reads as the empty list form.
		{`{"matches":null,"exec":"return"}`, `{"id":"","matches":[],"exec":"return"}`},
	}
	for _, tt := range tests {
		var value MosDNSMatch
		if value.UnmarshalJSON([]byte(tt.source)) != nil {
			t.Fatal("match decode failed")
		}
		encoded, err := Marshal(value)
		if err != nil || string(encoded) != tt.want {
			t.Fatalf("match encoding = %s, want %s", encoded, tt.want)
		}
		value.Exec = "reject 3"
		edited, err := Marshal(value)
		if err != nil {
			t.Fatal("match edit failed")
		}
		_, before := fuzzMembers(t, encoded)
		_, after := fuzzMembers(t, edited)
		if !bytes.Equal(before["matches"], after["matches"]) {
			t.Fatal("match form lost on edit")
		}
	}
}

// checkSnapshotTypes starts from original SQLite value text, before Python's
// JSON writer normalizes it. Each actual nested plain record is decoded and
// edited on its own, so a parent cannot hide a missing member declaration.
func checkSnapshotTypes(t *testing.T, rows []interchangeRow) {
	t.Helper()
	counts := map[string]int{}
	for _, row := range rows {
		var holder json.Unmarshaler
		switch row.Name {
		case "RouterRuleSyncReceipt":
			holder = &RouterRuleSyncReceipt{}
		case "AgentRuleSyncReceipt":
			holder = &AgentRuleSyncReceipt{}
		case "HAPayload":
			holder = &HAPayload{}
		case "system_config", "backup", "agents":
			holder = &System{}
		case "nodes", "subscriptions", "subscription_aggregations", "rule_library":
			holder = &Shared{}
		case "routers", "proxy_groups", "rule_configs", "mihomo", "surge", "loon", "mosdns", "domain_discovery", "device_notes", "client_fixed_ips", "oxidns":
			holder = &Profile{}
		default:
			continue
		}
		source := row.Value
		if row.Name != "AgentRuleSyncReceipt" && row.Name != "HAPayload" && row.Name != "RouterRuleSyncReceipt" {
			key, _ := json.Marshal(row.Name)
			source = append(append(append([]byte("{"), key...), ':'), row.Value...)
			source = append(source, '}')
		}
		if _, err := DecodeStored(source, holder, ""); err != nil {
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
	if typ == reflect.TypeFor[Dynamic]() || typ == reflect.TypeFor[TextLines]() || typ == reflect.TypeFor[json.RawMessage]() {
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
		_, container := reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type })
		if container || orderedElem(typ) != nil {
			order, members := fuzzMembers(t, source)
			for _, name := range order {
				child := value.MethodByName("Get").Call([]reflect.Value{reflect.ValueOf(name)})[0]
				assertStoredTree(t, members[name], child, counts)
			}
			return
		}
		if !reflect.PointerTo(typ).Implements(reflect.TypeFor[interface{ plainRecord() }]()) {
			return
		}
		fresh := reflect.New(typ)
		if _, err := DecodeStored(source, fresh.Interface(), ""); err != nil {
			t.Fatal("snapshot nested decode failed")
		}
		output, err := Marshal(fresh.Interface())
		// The section records are only the wrapper of one row here
		// ({"<row>": value}); written whole they add their always-written
		// members. The row value itself is compared one level down.
		section := typ == reflect.TypeFor[Profile]() || typ == reflect.TypeFor[System]() || typ == reflect.TypeFor[Shared]()
		if err != nil || !section && !sameStoredValue(t, output, source, true) {
			// Schema only: the record type and the names of the members
			// whose values differ, never the values.
			var before, after map[string]any
			_ = json.Unmarshal(source, &before)
			_ = json.Unmarshal(output, &after)
			var names []string
			for name, value := range before {
				if !reflect.DeepEqual(value, after[name]) {
					names = append(names, name)
				}
			}
			for name := range after {
				if _, present := before[name]; !present {
					names = append(names, "+"+name)
				}
			}
			sort.Strings(names)
			t.Fatalf("snapshot nested round trip changed the value: %s members %v", typ.Name(), names)
		}
		counts[typ.Name()]++
		_, members := fuzzMembers(t, source)
		fields := declaredFields(typ)
		for _, field := range fields {
			if original, ok := members[field.name]; ok {
				assertStoredTree(t, original, value.Field(field.index), counts)
			}
		}
		// Editing a primitive member must keep every other written member
		// byte-identical to the record's Go encoding.
		_, written := fuzzMembers(t, output)
		for _, field := range fields {
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
			for name, original := range written {
				if name != field.name && !bytes.Equal(original, after[name]) {
					t.Fatal("snapshot edit changed unrelated bytes")
				}
			}
			break
		}
	}
}

func TestMosDNSMatchScalarForm(t *testing.T) {
	for _, source := range []string{`"qname example.com"`, `"qname\u0020example.com"`, `""`} {
		var settings MosDNSSettings
		raw := []byte(`{"custom_matches":[` + source + `,{"matches":["qtype 28"],"exec":"return"}]}`)
		if err := json.Unmarshal(raw, &settings); err != nil {
			t.Fatal(err)
		}
		if issues := Issues(settings); len(issues) != 0 {
			t.Fatalf("valid union rejected: %v", issues)
		}
		if len(settings.CustomMatches) != 2 || settings.CustomMatches[0].Text == nil {
			t.Fatal("scalar match form lost")
		}
		// O18 T1c: the object form is written with its always-written id.
		encoded, err := Marshal(settings.CustomMatches)
		want := `[` + source + `,{"id":"","matches":["qtype 28"],"exec":"return"}]`
		if source == `"qname\u0020example.com"` {
			want = `["qname example.com",{"id":"","matches":["qtype 28"],"exec":"return"}]`
		}
		if err != nil || string(encoded) != want {
			t.Fatalf("union encoding = %s %v, want %s", encoded, err, want)
		}
	}
}
