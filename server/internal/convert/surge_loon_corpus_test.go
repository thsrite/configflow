package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/substore"
	"configflow/internal/yamlx/yamlxtest"
)

func loadJSONLines(t *testing.T, path string) []*jsonx.Object {
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

func rowOf(entry *jsonx.Object) mihomoRow {
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
		var out string
		var err error
		if row.fn == "surge" {
			out, err = convert.GenerateSurgeConfig(ctx, profile, args.GetString("base_url"))
		} else {
			out, err = convert.GenerateLoonConfig(ctx, profile, args.GetString("base_url"))
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
			mutatedEqual = yamlxtest.EqualJSON(e.Value("mutated"), profile)
		}
		if e.Has("mutated") && !mutatedEqual {
			return "mutated profile differs:\n  python " + encode(e.Value("mutated")) + "\n  go     " + encode(mihomoConv(profile))
		}
		return ""
	}
	args := e.GetList("args")
	arg := func(i int) any { return jsonx.DeepCopy(args[i]) }
	var got any
	var err error
	switch row.fn {
	case "node_surge":
		node, ok := arg(0).(*jsonx.Object)
		if !ok {
			return "non-object node"
		}
		var line, wg string
		line, wg, err = convert.ConvertNodeToSurge(node)
		got = []any{noneString(line), noneString(wg)}
	case "node_loon":
		var line string
		line, err = convert.ConvertNodeToLoon(arg(0).(*jsonx.Object))
		got = noneString(line)
	case "proxies_surge", "proxies_loon":
		proxies, _ := arg(0).([]any)
		if row.fn == "proxies_surge" {
			got, err = convert.ConvertProxiesToSurgeText(proxies, substore.ProxiesToNodes)
		} else {
			got, err = convert.ConvertProxiesToLoonText(proxies, substore.ProxiesToNodes)
		}
	case "group_surge":
		smart, _ := arg(3).(*jsonx.Object)
		var line string
		line, err = convert.ConvertProxyGroupToSurge(arg(0), arg(1).(*jsonx.Object), args[2].(string), smart)
		got = noneString(line)
	case "group_loon":
		var members []string
		for _, m := range args[1].([]any) {
			members = append(members, m.(string))
		}
		var line string
		line, err = convert.ConvertProxyGroupToLoon(arg(0), members)
		got = noneString(line)
	case "group_nodes":
		got, err = convert.GetProxyGroupNodes(arg(0), arg(1).(*jsonx.Object))
	case "build_rules":
		var lines, remote []string
		lines, remote, err = convert.BuildLoonRules(arg(0).(*jsonx.Object), args[1].(string))
		got = []any{stringsAny(lines), stringsAny(remote)}
	case "sanitize":
		got, err = convert.SanitizeLoonName(arg(0))
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
// through generate_surge_config / generate_loon_config (bytes, errors,
// mutations), random nodes, Mihomo proxy lists, groups, Loon rules and names.
func TestSurgeLoonCorpus(t *testing.T) {
	restore := convert.SetMihomoEnv(corpusEnv(nil, nil))
	defer restore()
	rows := loadJSONLines(t, "testdata/surge_loon_corpus.jsonl.gz")
	native := yamlxtest.ReaderCases(t, "../subs/testdata/n7_reader_expected.json.gz")
	counts := map[string]int{}
	typedStorage := map[string]int{}
	generated := map[string]int{}
	failures := 0
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
		counts[row.fn]++
		var profile *jsonx.Object
		if row.fn == "surge" || row.fn == "loon" {
			profile = entry.GetObject("args").GetObject("profile")
		} else if row.fn == "build_rules" {
			profile, _ = entry.GetList("args")[0].(*jsonx.Object)
		}
		if profile != nil {
			if issue := convert.CommonReplayIssue(profile); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		if profile != nil {
			if issue := convert.NodeProfileReplayIssue(profile); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		if profile != nil && (row.fn == "surge" || row.fn == "loon") {
			if issue := convert.DialerProfileReplayIssue(profile); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		if row.errClass == "" && (row.fn == "surge" || row.fn == "loon") {
			generated[row.fn]++
		}
		if msg := runSurgeLoonRow(t, row); msg != "" {
			failures++
			if failures <= 25 {
				t.Errorf("row %d (%s): %s\n  args %s", i, row.fn, msg, truncate(encode(entry.Value("args")), 600))
			}
		}
	}
	t.Logf("%d rows %v; generated configurations %v; %d differ; typed storage classes %v", len(rows), counts, generated, failures, typedStorage)
	if failures > 0 {
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + fmt.Sprintf("…(+%d)", len(r)-n)
	}
	return s
}
