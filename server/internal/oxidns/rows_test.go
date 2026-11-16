package oxidns

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

// Rows of testdata/recorded.jsonl.gz (c22_record.py) and
// testdata/corpus.jsonl.gz (gen.py): {"fn", "args": [...], "out" | "err":
// [class, message], "issues"?, "references"?, "unholdable"?}.

func TestMain(m *testing.M) {
	// schemas() reads backend/schemas/oxidns under the resource root.
	if os.Getenv("CONFIGFLOW_ROOT") == "" {
		os.Setenv("CONFIGFLOW_ROOT", "../../..")
	}
	os.Exit(m.Run())
}

func loadRows(t *testing.T, path string) []*jsonx.Object {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	var rows []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// errClass is the Python class of a Go error ("Pending" for a not yet
// ported function, "Unsupported" for the YAML refusal).
func errClass(err error) string {
	var u *UnsupportedYAMLError
	if errors.As(err, &u) {
		return "Unsupported"
	}
	if IsPending(err) {
		return "Pending"
	}
	if e, ok := AsError(err); ok {
		return e.Class
	}
	if class := convert.PyErrorClass(err); class != "" {
		return class
	}
	return fmt.Sprintf("%T", err)
}

// plain converts Go results to the recorded JSON shapes ([]byte ->
// {"$bytes": base64}, []string -> list, nil slices -> []).
func plain(v any) any {
	switch x := v.(type) {
	case []byte:
		return jsonx.Obj("$bytes", base64.StdEncoding.EncodeToString(x))
	case []string:
		return jsonx.ListOf(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = plain(item)
		}
		return out
	case *jsonx.Object:
		if x == nil {
			return nil
		}
		out := jsonx.NewObject()
		for k, item := range x.All() {
			out.Set(k, plain(item))
		}
		return out
	case []ManagedFile:
		out := []any{}
		for _, f := range x {
			out = append(out, plain(f.Object()))
		}
		return out
	case *string:
		if x == nil {
			return nil
		}
		return *x
	}
	return v
}

func compact(v any) string {
	data, err := jsonx.Encode(plain(v), jsonx.Options{Compact: true})
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(data)
}

func obj(v any) (*jsonx.Object, bool) {
	o, ok := v.(*jsonx.Object)
	return o, ok && o != nil
}

func objOrNil(v any) (*jsonx.Object, bool) {
	if v == nil {
		return nil, true
	}
	return obj(v)
}

// resolveSchemas replaces {"$schema": path} references by the schema
// objects of Schemas().
func resolveSchemas(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		if ref, ok := x.Get("$schema"); ok && x.Len() == 1 {
			global, plugins, err := Schemas()
			if err != nil {
				panic(err)
			}
			path := ref.([]any)
			var cur any = jsonx.Obj("global", global, "plugins", plugins)
			for _, part := range path {
				switch p := part.(type) {
				case string:
					cur = cur.(*jsonx.Object).Value(p)
				case jsonx.Number:
					i, _ := p.Int64()
					cur = cur.([]any)[i]
				}
			}
			return cur
		}
		out := jsonx.NewObject()
		for k, item := range x.All() {
			out.Set(k, resolveSchemas(item))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = resolveSchemas(item)
		}
		return out
	}
	return v
}

// callRow runs the Go counterpart of one row; ok=false means the arguments
// cannot be expressed in the Go signature.
func callRow(fn string, args []any) (out any, err error, ok bool) {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	text := func(i int) (string, bool) {
		s, ok := arg(i).(string)
		return s, ok
	}
	switch fn {
	case "normalize":
		validate, isBool := arg(1).(bool)
		if !isBool {
			return nil, nil, false
		}
		out, err = NormalizeSettings(arg(0), NormalizeOptions{SkipNativeArguments: !validate})
		return out, err, true
	case "safe_relative_path":
		label, ok := text(1)
		if !ok {
			return nil, nil, false
		}
		out, err = SafeRelativePath(arg(0), label)
		return out, err, true
	case "merge":
		defaults, ok1 := obj(arg(0))
		value, ok2 := obj(arg(1))
		if !ok1 || !ok2 {
			return nil, nil, false
		}
		return Merge(defaults, value), nil, true
	case "append_url_query":
		url, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		var params [][2]any
		for _, pair := range asList(arg(1)) {
			p, isList := pair.([]any)
			if !isList || len(p) != 2 {
				return nil, nil, false
			}
			params = append(params, [2]any{p[0], p[1]})
		}
		out, err = convert.AppendURLQuery(url, params)
		return out, err, true
	case "generate", "downloads", "custom_files":
		config, ok := obj(arg(0))
		base := ""
		if fn != "custom_files" {
			base, _ = text(1)
			if _, isText := arg(1).(string); !isText {
				ok = false
			}
		}
		if !ok {
			return nil, nil, false
		}
		switch fn {
		case "generate":
			out, err = GenerateOxidnsConfig(config, base)
		case "downloads":
			out, err = GetOxidnsRulesetDownloads(config, base)
		default:
			out, err = GetOxidnsCustomFiles(config)
		}
		return out, err, true
	case "unavailable_rulesets":
		config, ok := obj(arg(0))
		if !ok {
			return nil, nil, false
		}
		out, err = UnavailableRulesets(config, asList(arg(1)))
		return out, err, true
	case "ruleset_parts":
		item, ok := obj(arg(0))
		if !ok {
			return nil, nil, false
		}
		return RulesetParts(item), nil, true
	case "routing_groups":
		config, ok1 := obj(arg(0))
		settings, ok2 := obj(arg(1))
		if !ok1 || !ok2 {
			return nil, nil, false
		}
		legacy := LegacyNone
		switch arg(2) {
		case false:
		case true:
			legacy = LegacyAll
		case "content-aware":
			legacy = LegacyContentAware
		default:
			return nil, nil, false
		}
		items, given := arg(3).([]any)
		if arg(3) != nil && !given {
			return nil, nil, false
		}
		out, err = RoutingGroups(config, settings, legacy, items, given)
		return out, err, true
	case "hosts", "fallback":
		value, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		if fn == "hosts" {
			out, err = HostsFileContent(value)
		} else {
			out, err = FallbackUpstreams(value)
		}
		return out, err, true
	case "forward":
		value, ok := text(0)
		defaults, ok2 := stringList(arg(1))
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = Forward(value, defaults)
		return out, err, true
	case "items":
		config, ok := obj(arg(0))
		if !ok {
			return nil, nil, false
		}
		out, err = Items(config)
		return out, err, true
	case "base":
		config, ok := obj(arg(0))
		base, ok2 := text(1)
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = Base(config, base)
		return out, err, true
	case "tag":
		prefix, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		return TagFor(prefix, arg(1)), nil, true
	case "rule_path":
		item, ok := obj(arg(0))
		part, ok2 := text(1)
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = RulePath(item, part)
		return out, err, true
	case "load_native_yaml":
		label, ok := text(1)
		if !ok {
			return nil, nil, false
		}
		out, err = LoadNativeYAML(arg(0), label)
		return out, err, true
	case "managed_files":
		out, err = ManagedFiles(arg(0))
		return out, err, true
	case "native_document":
		settings, ok := obj(arg(0))
		validate, ok2 := arg(1).(bool)
		withSources, ok3 := arg(2).(bool)
		if !ok || !ok2 || !ok3 {
			return nil, nil, false
		}
		var sources *NativeSources
		if withSources {
			sources = &NativeSources{}
		}
		document, err := NativeDocument(settings, validate, sources)
		if err != nil {
			return nil, err, true
		}
		var src any
		if sources != nil {
			src = sources.Object()
		}
		return jsonx.Obj("document", document, "sources", src), nil, true
	case "resource_references", "validate_resource_files":
		document, ok := obj(arg(0))
		if !ok {
			return nil, nil, false
		}
		if fn == "resource_references" {
			out, err = ResourceReferences(document)
		} else {
			err = ValidateResourceFiles(document, asList(arg(1)))
		}
		return out, err, true
	case "relative_resource":
		out, err = RelativeResource(arg(0))
		return out, err, true
	case "reference_locations":
		tags, ok := stringList(arg(1))
		dynamic, ok2 := arg(2).(bool)
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = ReferenceLocations(arg(0), tags, dynamic)
		return out, err, true
	case "reference_reason":
		prefix, ok := text(1)
		references, ok2 := arg(0).([]any)
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = ReferenceReason(references, prefix)
		return out, err, true
	case "reference_error":
		message, ok := text(0)
		references, ok2 := arg(1).([]any)
		if !ok || !ok2 {
			return nil, nil, false
		}
		return NewReferenceError(message, references).Error(), nil, true
	case "read_match_targets":
		document, ok := obj(arg(0))
		settings, ok2 := obj(arg(1))
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = ReadMatchTargets(document, settings)
		return out, err, true
	case "normalize_action":
		parked, ok := arg(1).(bool)
		if !ok {
			return nil, nil, false
		}
		out, err = NormalizeAction(arg(0), parked)
		return out, err, true
	case "route_key":
		out, err = RouteKey(arg(0))
		return out, err, true
	case "route_exec":
		part, ok := text(1)
		if !ok {
			return nil, nil, false
		}
		out, err = RouteExec(arg(0), part)
		return out, err, true
	case "entry_key":
		out, err = EntryKey(arg(0), arg(1))
		return out, err, true
	case "helper_plugins":
		routes, ok := stringList(arg(0))
		actions, ok2 := objOrNil(arg(1))
		settings, ok3 := obj(arg(2))
		if !ok || !ok2 || !ok3 {
			return nil, nil, false
		}
		out, err = HelperPlugins(routes, actions, settings)
		return out, err, true
	case "normalize_client_range":
		out, err = NormalizeClientRange(arg(0))
		return out, err, true
	case "normalize_policies":
		out, err = NormalizePolicies(arg(0))
		return out, err, true
	case "validate_schema":
		schema, ok := obj(resolveSchemas(arg(1)))
		path, ok2 := arg(2).([]any)
		if !ok || !ok2 {
			return nil, nil, false
		}
		out, err = ValidateSchema(arg(0), schema, path)
		return out, err, true
	case "format_error":
		name, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		out, err = FormatError(name, arg(1))
		return out, err, true
	}
	return callRowC23(fn, args)
}

// rowResult classifies one row.
type rowResult int

const (
	rowSame rowResult = iota
	rowDiffers
	rowAccepted
	rowPending
	rowNotExpressible
)

func compareRow(entry *jsonx.Object) (string, rowResult) {
	fn := entry.GetString("fn")
	out, err, ok := callRow(fn, entry.GetList("args"))
	if !ok {
		return "", rowNotExpressible
	}
	if err != nil && IsPending(err) {
		return err.Error(), rowPending
	}
	if want := entry.GetList("err"); want != nil {
		if err == nil {
			return fmt.Sprintf("want %v, got output %s", compact(want), compact(out)), rowDiffers
		}
		class := errClass(err)
		if class == "Unsupported" && entry.GetBool("unholdable") {
			return "", rowAccepted
		}
		// Exception texts are compared only when Chinese (user-facing);
		// N2 moved URL parsing to net/url, whose error texts differ.
		if class != want[0] || (err.Error() != want[1] && strings.ContainsFunc(fmt.Sprint(want[1]), func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff })) {
			return fmt.Sprintf("want %s, got [%s %q]", compact(want), class, err.Error()), rowDiffers
		}
		if issues, present := entry.Get("issues"); present {
			e, _ := AsError(err)
			if e == nil || !e.HasIssues || compact(e.Issues) != compact(issues) {
				var got any
				if e != nil {
					got = e.Issues
				}
				return fmt.Sprintf("issues differ:\n got %s\nwant %s", compact(got), compact(issues)), rowDiffers
			}
		}
		if references, present := entry.Get("references"); present {
			e, _ := AsError(err)
			if e == nil || compact(e.References) != compact(references) {
				return "references differ", rowDiffers
			}
		}
		return "", rowSame
	}
	if err != nil {
		if entry.GetBool("unholdable") && errClass(err) == "Unsupported" {
			return "", rowAccepted
		}
		return fmt.Sprintf("want output, got [%s %q]", errClass(err), err.Error()), rowDiffers
	}
	want, _ := entry.Get("out")
	if got, expected := compact(out), compact(want); got != expected {
		if args := entry.GetList("args"); entry.GetString("fn") == "format_error" && want == nil && len(args) == 2 &&
			strings.ContainsAny(fmt.Sprint(args[1]), "{} ") {
			// Accepted (N2): net/url refuses a host with characters no
			// host name may hold ("$${X}" is a literal "${X}" host).
			return "", rowAccepted
		}
		return fmt.Sprintf("output differs:\n got %s\nwant %s", got, expected), rowDiffers
	}
	return "", rowSame
}

// replay runs every row of a file and reports per-function counts.
func replay(t *testing.T, path string, label func(*jsonx.Object) string) {
	rows := loadRows(t, path)
	failures, accepted, unsupported, notExpressible := 0, 0, 0, 0
	pendingBy := map[string]int{}
	counts := map[string]int{}
	same := map[string]int{}
	tests := map[string]bool{}
	for i, entry := range rows {
		fn := entry.GetString("fn")
		counts[fn]++
		tests[entry.GetString("test")] = true
		if entry.Has("unsupported") {
			unsupported++
			continue
		}
		msg, result := compareRow(entry)
		switch result {
		case rowSame:
			same[fn]++
		case rowNotExpressible:
			notExpressible++
		case rowAccepted:
			accepted++
		case rowPending:
			pendingBy[msg]++
		case rowDiffers:
			failures++
			if failures <= 25 {
				where := fmt.Sprintf("row %d", i)
				if label != nil {
					where = label(entry)
				}
				t.Errorf("%s (%s) %.400s: %s", where, fn, compact(entry.GetList("args")), msg)
			}
		}
	}
	pendingTotal := 0
	for _, n := range pendingBy {
		pendingTotal += n
	}
	t.Logf("identical: %v", same)
	t.Logf("%d rows from %d tests %v; %d unsupported, %d not expressible, %d accepted (unholdable YAML), %d pending %v, %d differ",
		len(rows), len(tests), counts, unsupported, notExpressible, accepted, pendingTotal, pendingBy, failures)
	if failures > 0 || notExpressible > 0 {
		t.Errorf("%d differ, %d not expressible of %d rows", failures, notExpressible, len(rows))
	}
}
