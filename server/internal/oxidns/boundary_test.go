package oxidns

// Boundary acceptance: the public entry points driven with the inputs the
// Python implementation was recorded with (test suite replays and random
// corpora under testdata, synthetic values only), outputs compared
// structurally (YAML texts parsed; ints, floats and bools kept apart) and
// rejections by whether they happen plus the exact Chinese message for
// OxidnsSettingsError and its subclasses.

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

const testdataDir = "testdata/"

// Seven avoids lining shards up with the corpus generator's four-call cycles.
const boundaryShards = 7

func TestMain(m *testing.M) {
	if os.Getenv("CONFIGFLOW_ROOT") == "" {
		os.Setenv("CONFIGFLOW_ROOT", "../../..")
	}
	os.Exit(m.Run())
}

type row struct {
	Test       string            `json:"test"`
	Fn         string            `json:"fn"`
	Args       []json.RawMessage `json:"args"`
	Out        json.RawMessage   `json:"out"`
	Err        []string          `json:"err"`
	Issues     json.RawMessage   `json:"issues"`
	References json.RawMessage   `json:"references"`
	Aliases    [][][]any         `json:"aliases"`
	ArgsAfter  []json.RawMessage `json:"args_after"`
	Unholdable json.RawMessage   `json:"unholdable"`
	Same       []bool            `json:"same"`
}

func readRows(t *testing.T, name string, fns map[string]bool) []row {
	t.Helper()
	var rows []row
	checked, total, err := scanRows(t.Context(), name, fns, func(r row) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d matching rows of %d input lines", name, checked, total)
	return rows
}

// scanRows reads every input line once, without race-only sampling. The visitor
// lets parallel suites bound their queue instead of retaining the entire corpus.
func scanRows(ctx context.Context, name string, fns map[string]bool, visit func(row) error) (checked, total int, err error) {
	file, err := os.Open(testdataDir + name)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", name, err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return 0, 0, fmt.Errorf("read gzip %s: %w", name, err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		index := total
		total++
		if err := ctx.Err(); err != nil {
			return checked, total, err
		}
		var r row
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return checked, total, fmt.Errorf("%s row %d: %w", name, index, err)
		}
		if fns[r.Fn] {
			if err := visit(r); err != nil {
				return checked, total, err
			}
			checked++
		}
	}
	if err := scanner.Err(); err != nil {
		return checked, total, fmt.Errorf("scan %s: %w", name, err)
	}
	return checked, total, nil
}

// parallelBoundary schedules input-local suites after sequential Setenv tests.
func parallelBoundary(t *testing.T) {
	t.Helper()
	t.Parallel()
}

// args decodes the arguments and restores recorded shared objects.
func (r row) args(t *testing.T) []*Node {
	t.Helper()
	out, err := r.decodeArgs()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (r row) decodeArgs() ([]*Node, error) {
	out := make([]*Node, len(r.Args))
	for i, raw := range r.Args {
		n, err := FromJSON(raw)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	resolve := func(path []any) (parent *Node, key any, node *Node) {
		node = out[int(path[0].(float64))]
		for _, step := range path[1:] {
			parent, key = node, step
			switch s := step.(type) {
			case string:
				node = get(node, s)
			case float64:
				node = items(node)[int(s)]
			}
		}
		return
	}
	for _, group := range r.Aliases {
		_, _, target := resolve(group[0])
		for _, path := range group[1:] {
			parent, key, _ := resolve(path)
			switch k := key.(type) {
			case nil:
				out[int(path[0].(float64))] = target
			case string:
				set(parent, k, target)
			case float64:
				parent.Content[int(k)] = target
			}
		}
	}
	return out, nil
}

func profileOf(n *Node) *Profile {
	if !isMap(n) {
		return NewProfile(nil)
	}
	p, err := ProfileFromJSON(ToJSON(n))
	if err != nil {
		return &Profile{doc: n, source: &model.Config{Profile: &model.Profile{}}}
	}
	p.doc = n
	return p
}

// toNode turns a Go result into a node for comparison.
func toNode(v any) *Node {
	switch x := v.(type) {
	case *Node:
		if x == nil {
			return Null()
		}
		return x
	case nil:
		return Null()
	case []*Node:
		return seqOf(x)
	case []File:
		out := L()
		for _, f := range x {
			item := M("path", f.Path, "content", string(f.Content))
			if f.Native {
				item = M("path", f.Path, "content", M("$bytes", base64.StdEncoding.EncodeToString(f.Content)), "native", true)
			}
			out.Content = append(out.Content, item)
		}
		return out
	case []RulesetDownload:
		out := L()
		for _, d := range x {
			item := M("name", d.Name, "url", d.URL, "local_path", d.LocalPath)
			if d.Behavior != nil {
				set(item, "behavior", d.Behavior)
			}
			out.Content = append(out.Content, item)
		}
		return out
	case CommonView:
		out := M("values", x.Values, "capabilities", toNode(x.Capabilities))
		set(out, "custom_match_targets", toNode(x.MatchTargets.Targets))
		set(out, "custom_match_target_tags", toNode(x.MatchTargets.Tags))
		return out
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		return L()
	}
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	n, err := FromJSON(data)
	if err != nil {
		panic(err)
	}
	return n
}

var yamlTextKeys = map[string]bool{"native_config": true, "template_base_config": true, "legacy_native_config": true,
	"local_dns": true, "remote_dns": true, "fallback_dns": true}

// canon parses YAML texts inside settings so formatting does not count.
func canon(n *Node, yamlText bool) *Node {
	switch {
	case isStr(n) && yamlText:
		if parsed, err := parseYAML(n.Value, errors.New("syntax")); err == nil {
			return M("$yaml", canon(parsed, false))
		}
	case isTag(n, tagUnsupported):
		return S(strings.Join(strings.Fields(n.Value), ""))
	case isMap(n):
		out := M()
		indices := make(map[string]int, len(n.Content)/2)
		pairs(n, func(k string, v *Node) bool {
			value := canon(v, yamlTextKeys[k])
			if index, exists := indices[k]; exists {
				out.Content[index] = value
			} else {
				indices[k] = len(out.Content) + 1
				out.Content = append(out.Content, S(k), value)
			}
			return true
		})
		return out
	case isSeq(n):
		out := L()
		for _, item := range n.Content {
			out.Content = append(out.Content, canon(item, false))
		}
		return out
	}
	return n
}

// firstDifference describes where two values differ.
func firstDifference(a, b *Node, at string) string {
	switch {
	case isMap(a) && isMap(b):
		for _, k := range orderedKeys(a, b) {
			if !has(a, k) || !has(b, k) {
				return fmt.Sprintf("%s.%s present go=%v py=%v", at, k, has(a, k), has(b, k))
			}
			if d := firstDifference(get(a, k), get(b, k), at+"."+k); d != "" {
				return d
			}
		}
		return ""
	case isSeq(a) && isSeq(b):
		for i := 0; i < len(a.Content) && i < len(b.Content); i++ {
			if d := firstDifference(a.Content[i], b.Content[i], fmt.Sprintf("%s[%d]", at, i)); d != "" {
				return d
			}
		}
		if len(a.Content) != len(b.Content) {
			return fmt.Sprintf("%s len go=%d py=%d", at, len(a.Content), len(b.Content))
		}
		return ""
	}
	if same(a, b) {
		return ""
	}
	return fmt.Sprintf("%s go=%s py=%s", at, short(a), short(b))
}

func short(n *Node) string {
	s := string(ToJSON(n))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

var settingsClasses = map[string]bool{"OxidnsSettingsError": true, "OxidnsValidationError": true, "OxidnsReferenceError": true}

// compare reports a difference ("" when equal).
func compare(r row, got *Node, err error) string {
	// N7-3d changes only the reader's policy/alias diagnostics here. Both
	// implementations must still reject as a settings error; accepted values,
	// business validation messages and argument post-state remain strict.
	if r.Err != nil && settingsClasses[r.Err[0]] && n73ReaderDiagnostic(err) {
		return ""
	}
	if r.Err != nil {
		if err == nil {
			return "python rejected (" + strings.Join(r.Err, ": ") + "), go accepted: " + short(got)
		}
		if settingsClasses[r.Err[0]] {
			message, ok := IsSettingsError(err)
			if !ok || message != r.Err[1] {
				return fmt.Sprintf("message go=%q py=%q", err.Error(), r.Err[1])
			}
			if r.Issues != nil {
				want, _ := FromJSON(r.Issues)
				var issues []Issue
				var v *ValidationError
				var s *SettingsError
				if errors.As(err, &v) {
					issues = v.Issues
				} else if errors.As(err, &s) {
					issues = s.Issues
				}
				if d := firstDifference(toNode(issues), want, "issues"); d != "" && !(len(issues) == 0 && len(want.Content) == 0) {
					return d
				}
			}
		}
		return ""
	}
	if err != nil {
		return "go rejected: " + err.Error()
	}
	want, _ := FromJSON(r.Out)
	yamlOut := r.Fn == "generate"
	return canonicalDifference(got, want, yamlOut, "out")
}

// canonicalDifference avoids reparsing unchanged YAML and rebuilding already
// equal trees. Exact structural equality is sufficient; otherwise the complete
// existing YAML canonical comparison still decides the result.
func canonicalDifference(got, want *Node, yamlText bool, path string) string {
	if identicalBoundaryTree(got, want) {
		return ""
	}
	return firstDifference(canon(got, yamlText), canon(want, yamlText), path)
}

// identicalBoundaryTree is deliberately stricter than semantic equality: map
// order, duplicate entries and scalar spellings must match too. That makes it a
// sufficient fast path without bypassing canonical duplicate/merge handling.
func identicalBoundaryTree(a, b *Node) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || len(a.Content) != len(b.Content) || a.Alias != nil || b.Alias != nil {
		return false
	}
	for i := range a.Content {
		if !identicalBoundaryTree(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

func boolArg(n *Node) bool { return isTrue(n) }

func strArg(n *Node) string { return str(n) }

func strList(n *Node) []string {
	var out []string
	for _, item := range items(n) {
		out = append(out, item.Value)
	}
	return out
}

func sourcesOf(n *Node) *Sources {
	s := &Sources{Plugins: []Origin{}}
	for _, item := range items(get(n, "plugins")) {
		index, _ := int64Of(get(item, "index"))
		s.Plugins = append(s.Plugins, Origin{str(get(item, "source")), int(index)})
	}
	for _, item := range items(get(n, "includes")) {
		s.Includes = append(s.Includes, Included{str(items(item)[0]), items(item)[1]})
	}
	return s
}

func referencesOf(n *Node) []Reference {
	var out []Reference
	for _, item := range items(n) {
		var path Path
		json.Unmarshal(ToJSON(get(item, "path")), &path)
		out = append(out, Reference{Path: path, Tag: str(get(item, "tag")), Reason: str(get(item, "reason"))})
	}
	return out
}

// call drives the Go entry point of one recorded Python function.
func call(t *testing.T, r row, a []*Node) (*Node, error, bool) {
	arg := func(i int) *Node {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	base := strArg(arg(1))
	switch r.Fn {
	case "generate":
		text, err := Generate(profileOf(a[0]), base)
		if err != nil {
			return nil, err, true
		}
		return S(text), nil, true
	case "downloads":
		d, err := RulesetDownloads(profileOf(a[0]), base)
		return toNode(d), err, true
	case "custom_files":
		f, err := CustomFiles(profileOf(a[0]))
		return toNode(f), err, true
	case "normalize":
		validate := len(a) < 2 || boolArg(a[1])
		n, err := NormalizeSettings(a[0], validate)
		return n, err, true
	case "load_native_yaml":
		n, err := loadNative(a[0], strArg(arg(1)))
		return n, err, true
	case "managed_files":
		f, err := ManagedFiles(a[0])
		return toNode(f), err, true
	case "native_document":
		sources := &Sources{}
		doc, err := NativeDocument(a[0], len(a) < 2 || boolArg(a[1]), sources)
		if err != nil {
			return nil, err, true
		}
		includes := L()
		for _, inc := range sources.Includes {
			includes.Content = append(includes.Content, L(inc.Name, inc.Globals))
		}
		if len(a) < 3 || !boolArg(a[2]) {
			return M("document", doc, "sources", Null()), nil, true
		}
		return M("document", doc, "sources", M("plugins", toNode(sources.Plugins), "includes", includes)), nil, true
	case "unavailable_rulesets":
		return toNode(UnavailableRulesets(profileOf(a[0]), strList(a[1]))), nil, true
	case "resource_references":
		refs, err := ResourceReferences(a[0])
		return toNode(refs), err, true
	case "validate_resource_files":
		var paths []string
		for _, item := range items(a[1]) {
			paths = append(paths, str(item)) // a non-text path is as invalid as ""
		}
		return Null(), ValidateResourceFiles(a[0], paths), true
	case "reference_locations":
		refs := ReferenceLocations(a[0], strList(arg(1)), len(a) < 3 || boolArg(a[2]))
		out := L()
		for _, ref := range refs {
			tag := S(ref.Tag)
			if ref.Tag == "" {
				tag = Null()
			}
			out.Content = append(out.Content, M("path", toNode(ref.Path), "tag", tag, "reason", ref.Reason))
		}
		return out, nil, true
	case "reference_reason":
		return S(ReferenceReason(referencesOf(a[0]), strArg(arg(1)))), nil, true
	case "read_match_targets":
		m, err := ReadMatchTargets(a[0], a[1])
		return toNode(m), err, true
	case "validate_native_config":
		return toNode(ValidateNativeConfig(a[0])), nil, true
	case "require_valid_document":
		issues, err := RequireValidDocument(a[0])
		return toNode(issues), err, true
	case "validate_expanded_document":
		return toNode(ValidateExpandedDocument(a[0], sourcesOf(a[1]))), nil, true
	case "validate_settings":
		v, err := ValidateSettings(a[0], profileOf(arg(1)), strArg(arg(2)))
		return toNode(v), err, true
	case "collect_plugin_issues":
		index, _ := int64Of(a[1])
		return toNode(collectPluginIssues(a[0], int(index))), nil, true
	case "format_error":
		return messageNode(FormatError(strArg(a[0]), a[1])), nil, true
	case "validate_plugin_format":
		if !isOneOf(a[0], "dns-record", "domain-expression") {
			return nil, errors.New("unsupported format"), true
		}
		return messageNode(ValidatePluginFormat(strArg(a[0]), a[1])), nil, true
	case "zone_records_error":
		return messageNode(zoneRecordsError(a[0], len(a) > 1 && boolArg(a[1]))), nil, true
	case "adguard_error":
		message, severity := adguardError(strArg(a[0]))
		return L(messageNode(message), severity), nil, true
	case "cron_error":
		return messageNode(cronError(a[0])), nil, true
	case "domain_error":
		return messageNode(domainError(a[0], len(a) > 1 && boolArg(a[1]))), nil, true
	case "regex_error":
		return messageNode(regexError(strArg(a[0]))), nil, true
	case "read_common_settings":
		v, err := ReadCommonSettings(a[0], profileOf(a[1]), strArg(arg(2)))
		return toNode(v), err, true
	case "apply_common_settings":
		applied, err := ApplyCommonSettings(a[0], a[1], profileOf(a[2]), strArg(arg(3)))
		if err != nil {
			return nil, err, true
		}
		out := toNode(applied.View)
		out.Content = append([]*Node{S("settings"), applied.Settings}, out.Content...)
		return out, nil, true
	case "read_routing":
		v, err := ReadRouting(a[0], a[1], profileOf(a[2]))
		return M("values", v.Values, "capabilities", toNode(v.Capabilities)), err, true
	case "apply_routing":
		opts := RoutingOptions{}
		if b := arg(5); b != nil && !isNone(b) {
			opts.BeforeConfig = profileOf(b)
		}
		var notices []string
		if n := arg(6); n != nil && isSeq(n) {
			notices = strList(n)
			opts.Notices = &notices
		}
		doc, settings, err := ApplyRouting(a[0], a[1], profileOf(a[2]), a[3], strArg(arg(4)), opts)
		if err != nil {
			return nil, err, true
		}
		out := L(doc, settings, nil)
		if opts.Notices != nil {
			out.Content[2] = strs(notices)
			a[6].Content = strs(notices).Content // the caller's list grows in place
		}
		return out, nil, true
	case "migrate_domain_only_routing":
		doc, settings, err := MigrateDomainOnlyRouting(a[0], a[1], profileOf(a[2]), strArg(arg(3)))
		return L(doc, settings), err, true
	case "remove_rule":
		n, err := RemoveRule(a[0], profileOf(a[1]), strArg(a[2]), strArg(arg(3)))
		return toNode(n), err, true
	case "resource_downloads":
		d, err := ResourceDownloads(a[0], a[1], profileOf(a[2]), strArg(arg(3)))
		return toNode(d), err, true
	case "ensure_local_dns_watchdog":
		n, err := EnsureLocalDNSWatchdog(a[0])
		return n, err, true
	case "owned_chains":
		out := M()
		for _, c := range ownedChainList(a[0]) {
			set(out, c.tag, c.action)
		}
		return out, nil, true
	case "refresh_rule_helpers":
		return Null(), RefreshRuleHelpers(a[0]), true
	case "resource_readers":
		return toNode(ResourceReaders(a[0])), nil, true
	case "resource_is_read":
		return B(ResourceIsRead(strArg(a[0]), strList(a[1]))), nil, true
	case "prepare_editor":
		n, err := PrepareEditor(a[0], profileOf(a[1]), strArg(arg(2)))
		return n, err, true
	case "preview_template":
		var resolutions *Node
		if n := arg(4); n != nil && !isNone(n) {
			resolutions = n
		}
		p, err := PreviewTemplate(a[0], a[1], profileOf(a[2]), strArg(arg(3)), resolutions)
		return M("settings", p.Settings, "changes", toNode(p.Changes), "conflicts", toNode(p.Conflicts)), err, true
	case "prepare_native_rule_updates":
		n, err := PrepareNativeRuleUpdates(a[0], a[1], profileOf(a[2]), strArg(arg(3)))
		return n, err, true
	case "read_policies":
		v, err := ReadPolicies(a[0], profileOf(a[1]))
		return M("policies", v.Policies, "editable", v.Editable, "reason", v.Reason, "side_entries", toNode(v.SideEntries)), err, true
	case "apply_policies":
		n, err := ApplyPolicies(a[0], profileOf(a[1]), a[2], strArg(arg(3)))
		return n, err, true
	case "reconcile_native_withdrawals":
		n, changed, err := ReconcileNativeWithdrawals(a[0], a[1], a[2], a[3], profileOf(a[4]))
		return L(n, changed), err, true
	case "rebase_native_rule_sources":
		changed, err := RebaseNativeRuleSources(profileOf(a[0]), profileOf(a[1]))
		return B(changed), err, true
	case "revision":
		return S(Revision(a[0])), nil, true
	case "stored_names":
		names, err := StoredNames(profileOf(a[0]))
		out := M()
		var keys []string
		for k := range names {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			set(out, k, S(names[k]))
		}
		return out, err, true
	case "inspect_entries":
		plugins, order := orderedPlugins(a[0])
		report, err := InspectEntries(a[0], plugins, order, profileOf(arg(1)))
		return toNode(report), err, true
	case "patch_document":
		n, err := PatchDocument(a[0], a[1], a[2], profileOrNil(arg(3)))
		return n, err, true
	case "template_inputs":
		return TemplateInputs(a[0], profileOrNil(arg(1))), nil, true
	}
	return nil, nil, false
}

func profileOrNil(n *Node) *Profile {
	if n == nil || isNone(n) {
		return nil
	}
	return profileOf(n)
}

func messageNode(message string) *Node {
	if message == "" {
		return Null()
	}
	return S(message)
}

// unordered outputs: Python built these from sets or dicts whose order is
// not part of the behaviour.
var unordered = map[string]bool{"stored_names": true}

var showLimit = func() int {
	if n, err := strconv.Atoi(os.Getenv("OXIDNS_SHOW")); err == nil {
		return n
	}
	return 3
}()

func runBoundary(t *testing.T, file string, fns ...string) {
	parallelBoundary(t)
	if only := os.Getenv("OXIDNS_FN"); only != "" {
		fns = strings.Split(only, ",")
	}
	wanted := map[string]bool{}
	for _, fn := range fns {
		wanted[fn] = true
	}
	ctx, cancel := context.WithCancel(t.Context())
	rows := make(chan row, boundaryShards)
	done := make(chan struct{})
	var checked, total int
	var readErr error
	go func() {
		defer close(done)
		defer close(rows)
		checked, total, readErr = scanRows(ctx, file, wanted, func(r row) error {
			select {
			case rows <- r:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	// Even Fatal/panic in a worker must cancel and join the producer. In the
	// successful case it has already closed the queue after the final row.
	defer func() { cancel(); <-done }()
	// Internal workers preserve the original externally reported test tree.
	var workers sync.WaitGroup
	for range boundaryShards {
		workers.Add(1)
		go func() {
			defer workers.Done()
			runBoundaryRows(t, file, rows)
		}()
	}
	workers.Wait()
	cancel()
	<-done
	if readErr != nil {
		t.Fatal(readErr)
	}
	t.Logf("%s: %d matching rows of %d input lines", file, checked, total)
}

func runBoundaryRows(t *testing.T, file string, rows <-chan row) {
	t.Helper()
	type stat struct{ same, differ, accepted int }
	stats := map[string]*stat{}
	shown := map[string]int{}
	for r := range rows {
		s := stats[r.Fn]
		if s == nil {
			s = &stat{}
			stats[r.Fn] = s
		}
		var got *Node
		var err error
		args, decodeErr := r.decodeArgs()
		if decodeErr != nil {
			t.Errorf("%s %s: decode arguments: %v", file, r.Test, decodeErr)
			continue
		}
		func() {
			defer func() {
				if p := recover(); p != nil {
					got, err = nil, fmt.Errorf("panic: %v", p)
				}
			}()
			got, err, _ = call(t, r, args)
		}()
		diff := compare(r, got, err)
		if diff == "" {
			diff = compareAfter(r, args, got)
		}
		if diff == "" {
			s.same++
			continue
		}
		// Python crashed (TypeError, AttributeError, IndexError, ...) on
		// malformed input that Go handles; and values a document cannot hold
		// (YAML sets/timestamps/non-string keys) that Go refuses.
		if r.Err != nil && !settingsClasses[r.Err[0]] && err == nil || r.Unholdable != nil && errors.Is(err, errUnsupportedYAML) || unicodeEdge(r) || nativeBoundaryDifference(r, args, got, err) {
			s.accepted++
			if os.Getenv("OXIDNS_ACCEPTED") != "" {
				args := ""
				if os.Getenv("OXIDNS_ARGS") != "" {
					data, _ := json.Marshal(r.Args)
					args = " args=" + string(data)
				}
				t.Logf("accepted %s %s: %s%s", file, r.Fn, diff, args)
			}
			continue
		}
		s.differ++
		if shown[r.Fn] < showLimit {
			shown[r.Fn]++
			args := ""
			if os.Getenv("OXIDNS_ARGS") != "" {
				data, _ := json.Marshal(r.Args)
				args = " args=" + string(data)
				if len(args) > 1500 {
					args = args[:1500] + "…"
				}
			}
			t.Errorf("%s %s (%s): %s%s", file, r.Fn, r.Test, diff, args)
			if os.Getenv("OXIDNS_FULL") != "" {
				t.Logf("got=%s want=%s", ToJSON(got), r.Out)
			}
		}
	}
	var names []string
	for fn := range stats {
		names = append(names, fn)
	}
	sort.Strings(names)
	for _, fn := range names {
		t.Logf("%s %-28s identical %6d differ %5d accepted %4d", file, fn, stats[fn].same, stats[fn].differ, stats[fn].accepted)
	}
}

var (
	c22Functions = []string{"generate", "downloads", "custom_files", "normalize", "load_native_yaml", "managed_files", "native_document",
		"unavailable_rulesets", "resource_references", "validate_resource_files", "reference_locations", "reference_reason", "read_match_targets", "format_error"}
	c23Functions = []string{"validate_native_config", "require_valid_document", "validate_expanded_document", "validate_settings",
		"collect_plugin_issues", "format_error", "validate_plugin_format", "zone_records_error", "adguard_error", "cron_error", "domain_error", "regex_error"}
	c24Functions = []string{"read_common_settings", "apply_common_settings"}
	c25Functions = []string{"read_routing", "apply_routing", "migrate_domain_only_routing", "remove_rule", "resource_downloads",
		"ensure_local_dns_watchdog", "owned_chains", "refresh_rule_helpers", "resource_readers", "resource_is_read"}
	c26Functions = []string{"prepare_editor", "preview_template", "prepare_native_rule_updates", "read_policies", "apply_policies",
		"reconcile_native_withdrawals", "rebase_native_rule_sources", "revision", "stored_names", "inspect_entries", "patch_document", "template_inputs"}
)

func TestBoundaryGenerationRecorded(t *testing.T) {
	runBoundary(t, "recorded.jsonl.gz", c22Functions...)
}
func TestBoundaryGenerationCorpus(t *testing.T) { runBoundary(t, "corpus.jsonl.gz", c22Functions...) }
func TestBoundaryValidationRecorded(t *testing.T) {
	runBoundary(t, "c23_recorded.jsonl.gz", c23Functions...)
}
func TestBoundaryValidationCorpus(t *testing.T) {
	runBoundary(t, "c23_corpus.jsonl.gz", c23Functions...)
}
func TestBoundaryCommonRecorded(t *testing.T) {
	runBoundary(t, "c24_recorded.jsonl.gz", c24Functions...)
}
func TestBoundaryCommonCorpus(t *testing.T) { runBoundary(t, "c24_corpus.jsonl.gz", c24Functions...) }
func TestBoundaryRoutingRecorded(t *testing.T) {
	runBoundary(t, "c25_recorded.jsonl.gz", c25Functions...)
}
func TestBoundaryRoutingCorpus(t *testing.T) { runBoundary(t, "c25_corpus.jsonl.gz", c25Functions...) }
func TestBoundaryEditorRecorded(t *testing.T) {
	runBoundary(t, "c26_recorded.jsonl.gz", c26Functions...)
}
func TestBoundaryEditorCorpus(t *testing.T) { runBoundary(t, "c26_corpus.jsonl.gz", c26Functions...) }

// unicodeEdge: inputs whose outcome depends on CPython-only Unicode rules
// (str.isdigit/isspace/split on characters Go classifies differently, IDNA
// 2003 vs UTS #46 for non-ASCII DNS names). Functional parity follows Go here.
func unicodeEdge(r row) bool {
	text := string(bytesOf(r.Args))
	// net/url refuses hosts urlsplit accepted (characters outside RFC 3986).
	if r.Fn == "format_error" && (strings.HasPrefix(text, `"upstream-address"`) || strings.HasPrefix(text, `"http-url"`)) && len(r.Args) == 2 && string(r.Args[1]) == `"$${X}"` && string(r.Out) == "null" {
		return true
	}
	for _, c := range text {
		switch {
		case c >= 0x1c && c <= 0x1f, c == 0x85:
			return true
		case c < 0x80:
		case unicode.IsSpace(c), unicode.In(c, unicode.Nd, unicode.No, unicode.Nl, unicode.Mn, unicode.Cf):
			return true
		}
	}
	return strings.Contains(text, "$ınclude") || strings.Contains(text, `\u001c`) || strings.Contains(text, `\u001d`) || strings.Contains(text, `\u001e`) || strings.Contains(text, `\u001f`)
}

// nativeBoundaryDifference checks deliberate native-library changes against the
// complete result, rather than waiving every row containing an unrelated Unicode name.
func nativeBoundaryDifference(r row, args []*Node, got *Node, err error) bool {
	// The editor now supports YAML merges, which Python's NativeLoader refused.
	if len(r.Err) > 1 && strings.HasSuffix(r.Err[1], " YAML 语法无效") {
		var containsMerge func(*Node) bool
		containsMerge = func(n *Node) bool {
			if n == nil {
				return false
			}
			if isStr(n) {
				var doc Node
				if yamlx.Unmarshal([]byte(n.Value), &doc) == nil {
					var walk func(*Node) bool
					walk = func(v *Node) bool {
						if v.Tag == "!!merge" {
							return true
						}
						for _, c := range v.Content {
							if walk(c) {
								return true
							}
						}
						return false
					}
					if walk(&doc) {
						return true
					}
				}
			}
			for _, c := range n.Content {
				if containsMerge(c) {
					return true
				}
			}
			return false
		}
		for _, a := range args {
			if containsMerge(a) {
				return true
			}
		}
	}
	if err == nil && len(r.Out) > 0 {
		want, e := FromJSON(r.Out)
		if e != nil {
			return false
		}
		// With an invalid URL net/url cannot derive its host. Schema validation
		// still rejects addr; omit only the consequent bootstrap diagnostic.
		if r.Fn == "validate_native_config" && len(args) > 0 && isSeq(want) {
			filtered := L()
			for _, issue := range want.Content {
				path := items(get(issue, "path"))
				omit := false
				if str(get(issue, "message")) == "引导 DNS 必须为 IP:端口 或 [IPv6]:端口" && len(path) == 6 {
					p, pok := int64Of(path[1])
					u, uok := int64Of(path[4])
					plugins := items(get(args[0], "plugins"))
					if pok && uok && p >= 0 && int(p) < len(plugins) {
						upstreams := items(at(plugins[p], "args", "upstreams"))
						if u >= 0 && int(u) < len(upstreams) {
							omit = FormatError("upstream-address", get(upstreams[u], "addr")) != ""
						}
					}
				}
				if !omit {
					filtered.Content = append(filtered.Content, issue)
				}
			}
			if same(filtered, got) {
				return true
			}
		}
	}
	return false
}

func bytesOf(args []json.RawMessage) []byte {
	var out []byte
	for _, a := range args {
		var v any
		json.Unmarshal(a, &v)
		data, _ := json.Marshal(v)
		out = append(out, data...)
	}
	return out
}

// compareAfter checks arguments edited in place (args_after) and, for
// migrate_domain_only_routing, that unchanged inputs are returned as such.
func compareAfter(r row, args []*Node, got *Node) string {
	for i, raw := range r.ArgsAfter {
		if string(raw) == "null" || i >= len(args) {
			continue
		}
		want, _ := FromJSON(raw)
		if d := canonicalDifference(args[i], want, false, fmt.Sprintf("args_after[%d]", i)); d != "" {
			return d
		}
	}
	if len(r.Same) == 2 && isSeq(got) {
		for i, identical := range r.Same {
			if (got.Content[i] == args[i]) != identical {
				return fmt.Sprintf("returned argument %d identity go=%v py=%v", i, got.Content[i] == args[i], identical)
			}
		}
	}
	return ""
}
