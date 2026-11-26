package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/testjson/modeldirt"
	"configflow/internal/yamlx"
)

func loadJSONLines(t *testing.T, path string) []*convert.JObj {
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
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	var rows []*convert.JObj
	for scanner.Scan() {
		value, err := convert.ParseJ(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*convert.JObj))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func loadGzipJObj(t testing.TB, path string) *convert.JObj {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	data, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	v, err := convert.ParseJ(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*convert.JObj)
}

func rowOf(entry *convert.JObj) mihomoRow {
	row := mihomoRow{entry: entry, fn: entry.GetString("fn")}
	if e := entry.GetList("err"); e != nil {
		row.errClass, row.errMsg = e[0].(string), e[1].(string)
	}
	return row
}

// noneString is Python's None/str result: "" stands for None.
func noneString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func compareOut(row mihomoRow, got any) string {
	if !sameValue(row.entry.Value("out"), got) {
		return "differs:\n  python " + encode(row.entry.Value("out")) + "\n  go     " + encode(mihomoConv(got))
	}
	return ""
}

func stringsAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func decodeClientConfig(profile *convert.JObj) (*model.Config, error) {
	if err := convert.ValidateResourceArraysJ(profile); err != nil {
		return nil, err
	}
	return convert.DecodeConfigJ(profile)
}

func decodeYAMLProxies(list []any) ([]*yamlx.Node, error) {
	nodes := make([]*yamlx.Node, len(list))
	for i, item := range list {
		n, err := convert.YAMLNodeFromJ(item)
		if err != nil {
			return nil, err
		}
		nodes[i] = n
	}
	return nodes, nil
}

// runSurgeLoonRow runs one row of testdata/surge_loon_corpus.jsonl.gz (also
// used by the recorded replay for the helper calls).
func runSurgeLoonRow(t *testing.T, row mihomoRow) string {
	t.Helper()
	ctx := context.Background()
	e := row.entry
	switch row.fn {
	case "surge", "loon":
		args := e.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		cfg, err := decodeClientConfig(profile)
		var out string
		if err == nil {
			if row.fn == "surge" {
				out, err = convert.SurgeConfig(ctx, cfg, args.GetString("base_url"))
			} else {
				out, err = convert.LoonConfig(ctx, cfg, args.GetString("base_url"))
			}
		}
		if msg := sameError(t, row, err); msg != "" {
			return msg
		}
		if err == nil {
			if want := e.GetString("out"); want != out {
				return "config differs:\n" + firstDiff(want, out)
			}
		}
		mutatedEqual := sameValue(e.Value("mutated"), profile)
		if e.GetBool("native_yaml_oracle") {
			mutatedEqual = convert.EqualJ(e.Value("mutated"), profile)
		}
		if e.Has("mutated") && !mutatedEqual {
			return "mutated profile differs:\n  python " + encode(e.Value("mutated")) + "\n  go     " + encode(mihomoConv(profile))
		}
		return ""
	}
	args := e.GetList("args")
	arg := func(i int) any { return convert.DeepCopyJ(args[i]) }
	var got any
	var err error
	switch row.fn {
	case "node_surge":
		nodeObj, ok := arg(0).(*convert.JObj)
		if !ok {
			return "non-object node"
		}
		var node model.Node
		if node, err = convert.DecodeJ[model.Node](nodeObj); err == nil {
			var line, wg string
			line, wg, err = convert.SurgeNode(node)
			got = []any{noneString(line), noneString(wg)}
		}
	case "node_loon":
		var node model.Node
		if node, err = convert.DecodeJ[model.Node](arg(0)); err == nil {
			var line string
			line, err = convert.LoonNode(node)
			got = noneString(line)
		}
	case "proxies_surge", "proxies_loon":
		proxies, _ := arg(0).([]any)
		var nodes []*yamlx.Node
		if nodes, err = decodeYAMLProxies(proxies); err == nil {
			target := "surge"
			if row.fn == "proxies_loon" {
				target = "loon"
			}
			got, err = convert.ClientProxyText(nodes, substore.ProxyNodes, target)
		}
	case "group_surge":
		var group model.ProxyGroup
		var cfg *model.Config
		var smart map[string]string
		if group, err = convert.DecodeJ[model.ProxyGroup](arg(0)); err == nil {
			if cfg, err = convert.DecodeConfigJ(arg(1).(*convert.JObj)); err == nil {
				if arg(3) != nil {
					smart, err = convert.DecodeJ[map[string]string](arg(3))
				}
				if err == nil {
					var line string
					line, err = convert.SurgeGroup(group, cfg, args[2].(string), smart)
					got = noneString(line)
				}
			}
		}
	case "group_loon":
		var members []string
		for _, m := range args[1].([]any) {
			members = append(members, m.(string))
		}
		var group model.ProxyGroup
		if group, err = convert.DecodeJ[model.ProxyGroup](arg(0)); err == nil {
			var line string
			line, err = convert.LoonGroup(group, members)
			got = noneString(line)
		}
	case "group_nodes":
		var group model.ProxyGroup
		var cfg *model.Config
		if group, err = convert.DecodeJ[model.ProxyGroup](arg(0)); err == nil {
			if cfg, err = convert.DecodeConfigJ(arg(1).(*convert.JObj)); err == nil {
				var names []string
				if names, err = convert.ClientGroupNodes(group, cfg); err == nil {
					got = stringsAny(names)
				}
			}
		}
	case "build_rules":
		var cfg *model.Config
		if cfg, err = convert.DecodeConfigJ(arg(0).(*convert.JObj)); err == nil {
			var lines, remote []string
			if lines, remote, err = convert.LoonRules(cfg, args[1].(string)); err == nil {
				got = []any{stringsAny(lines), stringsAny(remote)}
			}
		}
	case "sanitize":
		var text string
		if text, err = convert.DecodeJ[string](arg(0)); err == nil {
			got = convert.ClientSanitizeNameForTest(text)
		}
	default:
		return "unknown fn " + row.fn
	}
	if msg := sameError(t, row, classed(err)); msg != "" || err != nil {
		return msg
	}
	return compareOut(row, got)
}

// classed turns errors of other packages that know their Python class
// (substore.PyError from proxies_to_nodes) into a replayedError, which
// pythonClass reads.
func classed(err error) error {
	var c interface{ PythonClass() string }
	if err != nil && errors.As(err, &c) {
		return &replayedError{class: c.PythonClass(), message: err.Error()}
	}
	return err
}

// TestSurgeLoonCorpus compares the Surge and Loon converters with CPython on
// testdata/surge_loon_corpus.jsonl.gz (gen_surge_loon.py): random profiles
// through SurgeConfig / LoonConfig (bytes, errors, mutations), random nodes,
// Mihomo proxy lists, groups, Loon rules and names.
func TestSurgeLoonCorpus(t *testing.T) {
	nativeSubs := nativeSubsCallers(t, "surge_loon_corpus")
	restore := convert.OverrideMihomoEnvironmentForTest(t, corpusEnv(nil, nil))
	defer restore()
	rows := loadJSONLines(t, "testdata/surge_loon_corpus.jsonl.gz")
	native := loadGzipJObj(t, "../subs/testdata/n7_reader_expected.json.gz").GetObject("cases")
	counts := map[string]int{}
	typedStorage := map[string]int{}
	generated := map[string]int{}
	failures := 0
	o18Accepted := map[string]int{}
	defer func() { t.Logf("accepted O18 T1c differences: %v", o18Accepted) }()
	index := readClientClassifications(t, "surge_loon_corpus.jsonl.gz", clientIndexPath)
	var classifications []clientClassification
	usedIndex := map[int]bool{}
	for i, entry := range rows {
		recorded := native.GetObject(fmt.Sprintf("convert/surge_loon/%d", i))
		if recorded == nil {
			t.Fatalf("missing native YAML oracle row %d", i)
		}
		if recorded.GetBool("changed") {
			if len(recorded.GetList("classes")) == 0 {
				t.Fatalf("unclassified YAML oracle row %d", i)
			}
			expected := recorded.GetObject("expected").GetObject("value")
			if expected == nil {
				t.Fatalf("missing converter payload %d", i)
			}
			entry = entry.DeepCopy()
			entry.Set("native_yaml_oracle", true)
			for _, field := range []string{"out", "err", "mutated"} {
				entry.Delete(field)
				if expected.Has(field) {
					entry.Set(field, expected.Value(field))
				}
			}
		}
		row := rowOf(entry)
		if expected, ok := nativeSubs[i]; ok {
			row.errClass, row.errMsg = expected.NativeError[0], expected.NativeError[1]
		}
		counts[row.fn]++
		// Existing typed decoder cases remain classified, but every representable
		// row still executes its public entry below before an oracle decision.
		issue := clientEntryIssue(t, entry)
		classification := clientClassification{Row: i, Fn: row.fn, InputSHA: generationDigest([]byte(encode(entry.Value("args")))), Field: issue, Family: clientIssueFamily(issue)}
		if issue != "" {
			classifications = append(classifications, classification)
		}
		if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") != "1" {
			expected, found := index[i]
			if found {
				usedIndex[i] = true
			}
			if (issue != "") != found || (found && classification != expected) {
				t.Errorf("row %d: helper input classification changed: %s", i, issue)
			}
		}
		if row.errClass == "" && (row.fn == "surge" || row.fn == "loon") {
			generated[row.fn]++
		}
		if issue != "" {
			typedStorage[issue]++
			// Non-object nodes cannot enter the typed record boundary;
			// they have already been rejected by the actual typed record decoder.
			if row.fn == "node_loon" || row.fn == "node_surge" {
				if _, ok := entry.GetList("args")[0].(*convert.JObj); !ok {
					continue
				}
			}
			_, _ = executeClientBoundary(t, entry)
			continue
		}
		if msg := runSurgeLoonRow(t, row); msg != "" {
			if dirt := clientRowDirt(entry); len(dirt) > 0 {
				// Accepted O18 T1c difference: the stored profile, node,
				// group or subscription proxy holds dirty data that plain
				// records read differently (port 1.5 or null reads as 0,
				// a number in a text member as its text, null as missing).
				o18Accepted[row.fn+" "+strings.Join(dirt, "+")]++
				continue
			}
			failures++
			if failures <= 25 {
				t.Errorf("row %d (%s): %s\n  args %s", i, row.fn, msg, truncate(encode(entry.Value("args")), 600))
			}
		}
	}
	if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") != "1" && len(usedIndex) != len(index) {
		t.Fatal("unused helper classification rows")
	}
	if failures == 0 {
		writeClientClassifications(t, "surge_loon_corpus.jsonl.gz", clientIndexPath, classifications)
	}
	pending := 0
	for _, entry := range classifications {
		if entry.Family == "pending shared integer-string" {
			pending++
		}
	}
	t.Logf("%d rows %v; generated configurations %v; strict=%d classified=%d field_classes=%d pending_shared=%d unknown=%d", len(rows), counts, generated, len(rows)-len(classifications), len(classifications), len(typedStorage), pending, failures)
	if failures > 0 {
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
}

// clientRowDirt returns the O18 T1c dirt kinds (without empty text) of a
// Surge/Loon corpus row's stored input: the profile of a generated
// configuration or of build_rules/group rows, a node, a group, or the
// subscription proxies read as nodes.
func clientRowDirt(entry *convert.JObj) []string {
	args := entry.GetList("args")
	kinds := map[string]bool{}
	add := func(list []string) {
		for _, kind := range list {
			if kind != modeldirt.DirtEmptyText {
				kinds[kind] = true
			}
		}
	}
	of := func(value any, sample any) {
		data, err := convert.CompactJ(value)
		if err == nil {
			add(modeldirt.Of(data, sample))
		}
	}
	switch entry.GetString("fn") {
	case "surge", "loon":
		add(convert.O18StoredDirt(entry.GetObject("args").GetObject("profile")))
	case "node_surge", "node_loon":
		of(args[0], model.Node{})
	case "group_surge", "group_nodes", "group_loon":
		of(args[0], model.ProxyGroup{})
		if len(args) > 1 {
			if profile, ok := args[1].(*convert.JObj); ok {
				add(convert.O18StoredDirt(profile))
			}
		}
	case "build_rules":
		if profile, ok := args[0].(*convert.JObj); ok {
			add(convert.O18StoredDirt(profile))
		}
	case "proxies_surge", "proxies_loon":
		of(args[0], []model.Node{})
	}
	out := make([]string, 0, len(kinds))
	for kind := range kinds {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + fmt.Sprintf("…(+%d)", len(r)-n)
	}
	return s
}
