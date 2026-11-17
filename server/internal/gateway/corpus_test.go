package gateway_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// corpusRow is one line of testdata/a19_corpus.jsonl.gz (see gen_a19.py).
type corpusRow struct {
	FN    string          `json:"fn"`
	Args  json.RawMessage `json:"args"`
	Out   json.RawMessage `json:"out"`
	Err   []string        `json:"err"`
	Loads []string        `json:"loads"`
}

func loadCorpus(t *testing.T) []corpusRow {
	t.Helper()
	file, err := os.Open("testdata/a19_corpus.jsonl.gz")
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
	var rows []corpusRow
	for scanner.Scan() {
		var row corpusRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// args decodes a row's arguments.
func args[T any](t *testing.T, row corpusRow) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(row.Args, &value); err != nil {
		t.Fatalf("%s args: %v", row.FN, err)
	}
	return value
}

// jsonxValue parses JSON into the jsonx model (the adapter's input).
func jsonxValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	value, err := jsonx.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func jsonxObject(t *testing.T, raw json.RawMessage) *jsonx.Object {
	t.Helper()
	obj, _ := jsonxValue(t, raw).(*jsonx.Object)
	if obj == nil {
		obj = jsonx.NewObject()
	}
	return obj
}

// node reads JSON as a YAML node tree.
func node(t *testing.T, raw json.RawMessage) *yaml.Node {
	t.Helper()
	n, err := yamlx.FromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// loader serves a row's loader map (missing: "") and records the calls.
func loader(texts map[string]string, calls *[]string) gateway.ContentLoader {
	return func(_ context.Context, item, _ *gateway.Rule) (string, error) {
		if calls != nil {
			*calls = append(*calls, item.ID())
		}
		return texts[item.ID()], nil
	}
}

// errorMismatch compares a Go error with Python's [class, detail]: a
// LinuxGatewayError by message, a RouterSplitError by code, any other
// (unintended) exception only as "rejected, but not as a gateway error".
func errorMismatch(err error, want []string) string {
	if want == nil {
		if err != nil {
			return fmt.Sprintf("Python succeeded, Go: %v", err)
		}
		return ""
	}
	if err == nil {
		return fmt.Sprintf("Python raised %v, Go succeeded", want)
	}
	var split *routersplit.RouterSplitError
	var gwErr *gateway.Error
	switch want[0] {
	case "RouterSplitError":
		if !errors.As(err, &split) || split.Code != want[1] {
			return fmt.Sprintf("want RouterSplitError %s, Go: %v", want[1], err)
		}
	case "LinuxGatewayError":
		if errors.As(err, &split) || !errors.As(err, &gwErr) || gwErr.Message != want[1] {
			return fmt.Sprintf("want LinuxGatewayError %q, Go: %T %v", want[1], err, err)
		}
	default:
		if errors.As(err, &gwErr) {
			return fmt.Sprintf("Python crashed with %v, Go refused as gateway error %q", want, gwErr.Message)
		}
	}
	return ""
}

// sameEntry compares rule provider entries (Go first): equal, or equal
// field by field where differing fields are the same IP network. IPv6 text
// forms differ (Python writes ::ffff:102:304, Go ::ffff:1.2.3.4), and Python
// kept an IPv6 zone in a /128 ("fe80::1%eth0/128", which Mihomo cannot
// parse) where Go writes the network without it.
func sameEntry(a, b string) bool {
	if a == b {
		return true
	}
	fa, fb := strings.Split(a, ","), strings.Split(b, ",")
	if len(fa) != len(fb) {
		return false
	}
	for i := range fa {
		if fa[i] == fb[i] {
			continue
		}
		pa, errA := netip.ParsePrefix(fa[i])
		zoned, bits, _ := strings.Cut(fb[i], "/")
		zoned, _, _ = strings.Cut(zoned, "%")
		pb, errB := netip.ParsePrefix(zoned + "/" + bits)
		if errA != nil || errB != nil || pa != pb {
			return false
		}
	}
	return true
}

func samePayloadYAML(t *testing.T, goText, pyText string) bool {
	t.Helper()
	var goDoc, pyDoc struct {
		Payload []string `yaml:"payload"`
	}
	if err := yamlx.Unmarshal([]byte(goText), &goDoc); err != nil {
		t.Fatalf("Go YAML: %v\n%s", err, goText)
	}
	if err := yamlx.Unmarshal([]byte(pyText), &pyDoc); err != nil {
		t.Fatalf("Python YAML: %v", err)
	}
	return slices.EqualFunc(goDoc.Payload, pyDoc.Payload, sameEntry)
}

// sameJSON compares two JSON documents by value.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(va, vb)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type configArgs struct {
	Config json.RawMessage   `json:"config"`
	RuleID string            `json:"rule_id"`
	Loader map[string]string `json:"loader"`
}

// generationEnv makes the Mihomo generator work offline, with the gateway
// and router split hooks as in production.
func generationEnv(t *testing.T) {
	t.Helper()
	env := mihomoenv.Env()
	env.Converter = nil
	env.LoadSubscriptionCache = func(context.Context, string) (*jsonx.Object, error) { return nil, nil }
	env.DownloadedText = func(context.Context, string) (string, bool) { return "", false }
	env.PrepareProviderBundle = nil
	t.Cleanup(convert.SetMihomoEnv(env))
	hooks := gateway.Hooks()
	hooks.IsFullTun = func(config *jsonx.Object) (bool, error) { return routersplit.IsFullTun(config), nil }
	t.Cleanup(convert.RegisterMihomoHooks(hooks))
}

func TestPythonCorpus(t *testing.T) {
	rows := loadCorpus(t)
	nativeReferences := nativeGatewayReferences(t)
	generationEnv(t)
	ctx := context.Background()
	counts := map[string]int{}
	failures := 0
	fail := func(i int, row corpusRow, format string, a ...any) {
		failures++
		if failures <= 40 {
			args := string(row.Args)
			if len(args) > 600 {
				args = args[:600] + "…"
			}
			t.Errorf("row %d %s: %s\nargs: %s", i, row.FN, fmt.Sprintf(format, a...), args)
		}
	}
	for i, row := range rows {
		row = nativeGatewayRow(t, i, row, nativeReferences)
		counts[row.FN]++
		switch row.FN {
		case "normalize":
			a := args[struct {
				Value json.RawMessage `json:"value"`
			}](t, row)
			settings, err := gateway.NormalizeSettings(a.Value)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil && !sameJSON(t, mustJSON(t, settings), row.Out) {
				fail(i, row, "Go %s, Python %s", mustJSON(t, settings), row.Out)
			}
		case "fake_ip_range":
			a := args[struct {
				Value string `json:"value"`
			}](t, row)
			network, err := gateway.ParseFakeIPRange(a.Value)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil && !sameJSON(t, mustJSON(t, network.String()), row.Out) {
				fail(i, row, "Go %s, Python %s", network, row.Out)
			}
		case "fake_ip_network":
			a := args[struct {
				Doc json.RawMessage `json:"doc"`
			}](t, row)
			network, err := gateway.FakeIPNetwork(node(t, a.Doc))
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil && !sameJSON(t, mustJSON(t, network.String()), row.Out) {
				fail(i, row, "Go %s, Python %s", network, row.Out)
			}
		case "routing_mark":
			a := args[struct {
				Value json.RawMessage `json:"value"`
				Root  string          `json:"root"`
			}](t, row)
			got := gateway.RoutingMarkPath(node(t, a.Value), a.Root)
			gotJSONX := gateway.RoutingMarkPathJSONX(jsonxValue(t, a.Value), a.Root)
			want := ""
			_ = json.Unmarshal(row.Out, &want)
			if got != want || gotJSONX != want {
				fail(i, row, "Go %q / %q, Python %q", got, gotJSONX, want)
			}
		case "extract":
			a := args[struct {
				Content  string `json:"content"`
				Behavior string `json:"behavior"`
			}](t, row)
			networks, err := gateway.ExtractCIDRs(a.Content, a.Behavior)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
				continue
			}
			if err == nil {
				got := make([]string, len(networks))
				for j, n := range networks {
					got[j] = n.String()
				}
				if !sameJSON(t, mustJSON(t, got), row.Out) {
					fail(i, row, "Go %v, Python %s", got, row.Out)
				}
			}
		case "provider_payload", "provider_yaml", "source_yaml":
			a := args[configArgs](t, row)
			config := jsonxObject(t, a.Config)
			before, _ := jsonx.Compact(config)
			c := gateway.NewConfig(config)
			load := loader(a.Loader, nil)
			var err error
			var mismatch bool
			var got any
			switch row.FN {
			case "provider_payload":
				var payload []string
				payload, err = c.ProviderPayload(ctx, a.RuleID, load)
				got = payload
				mismatch = err == nil && row.Err == nil && !sameJSON(t, mustJSON(t, payload), row.Out)
			default:
				var text string
				if row.FN == "provider_yaml" {
					text, err = c.ProviderYAML(ctx, a.RuleID, load)
				} else {
					text, err = c.SourceYAML(ctx, a.RuleID, load)
				}
				got = text
				var want string
				_ = json.Unmarshal(row.Out, &want)
				mismatch = err == nil && row.Err == nil && !samePayloadYAML(t, text, want)
			}
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if mismatch {
				fail(i, row, "Go %v, Python %s", got, row.Out)
			}
			if after, _ := jsonx.Compact(config); string(after) != string(before) {
				fail(i, row, "configuration modified")
			}
		case "provider_path":
			a := args[struct {
				Config json.RawMessage `json:"config"`
				RuleID string          `json:"rule_id"`
				Source bool            `json:"source"`
			}](t, row)
			got := gateway.NewConfig(jsonxObject(t, a.Config)).ProviderPath(a.RuleID, a.Source)
			if !sameJSON(t, mustJSON(t, got), row.Out) {
				fail(i, row, "Go %q, Python %s", got, row.Out)
			}
		case "provider_name":
			a := args[configArgs](t, row)
			if got := gateway.ProviderName(a.RuleID); !sameJSON(t, mustJSON(t, got), row.Out) {
				fail(i, row, "Go %q, Python %s", got, row.Out)
			}
		case "overlap":
			a := args[configArgs](t, row)
			var calls []string
			warnings, err := gateway.NewConfig(jsonxObject(t, a.Config)).DirectOverlapWarnings(ctx, loader(a.Loader, &calls))
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
				continue
			}
			if err == nil && !sameJSON(t, mustJSON(t, warnings), row.Out) {
				fail(i, row, "Go %q, Python %s", warnings, row.Out)
			}
			if row.Loads == nil {
				row.Loads = []string{}
			}
			if calls == nil {
				calls = []string{}
			}
			if !slices.Equal(calls, row.Loads) {
				fail(i, row, "loader calls Go %q, Python %q", calls, row.Loads)
			}
		case "editor_state":
			a := args[struct {
				Config   json.RawMessage `json:"config"`
				Guidance bool            `json:"guidance"`
			}](t, row)
			state, err := gateway.NewConfig(jsonxObject(t, a.Config)).EditorState(a.Guidance)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil && !sameJSON(t, mustJSON(t, state), row.Out) {
				fail(i, row, "Go %s\nPython %s", mustJSON(t, state), row.Out)
			}
		case "validate_ids":
			a := args[struct {
				Config   json.RawMessage `json:"config"`
				IDs      []string        `json:"ids"`
				Previous []string        `json:"previous"`
			}](t, row)
			err := gateway.NewConfig(jsonxObject(t, a.Config)).ValidateRuleSetIDs(a.IDs, a.Previous)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			}
		case "validate_selection":
			a := args[struct {
				Config json.RawMessage `json:"config"`
				Value  json.RawMessage `json:"value"`
			}](t, row)
			config := jsonxObject(t, a.Config)
			before, _ := jsonx.Compact(config)
			settings, err := gateway.NewConfig(config).ValidateSelection(a.Value)
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil && !sameJSON(t, mustJSON(t, settings), row.Out) {
				fail(i, row, "Go %s, Python %s", mustJSON(t, settings), row.Out)
			}
			if after, _ := jsonx.Compact(config); string(after) != string(before) {
				fail(i, row, "configuration modified")
			}
		case "apply":
			a := args[struct {
				Doc     json.RawMessage `json:"doc"`
				Config  json.RawMessage `json:"config"`
				BaseURL string          `json:"base_url"`
				Auth    bool            `json:"auth"`
			}](t, row)
			doc, config := jsonxObject(t, a.Doc), jsonxObject(t, a.Config)
			before, _ := jsonx.Compact(config)
			restore := gateway.SetAuthEnabled(a.Auth)
			err := gateway.ApplyJSONX(doc, config, a.BaseURL)
			restore()
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil {
				got, _ := jsonx.Compact(doc)
				want, _ := jsonx.Compact(jsonxValue(t, row.Out))
				if string(got) != string(want) {
					fail(i, row, "Go %s\nPython %s", got, want)
				}
			}
			if after, _ := jsonx.Compact(config); string(after) != string(before) {
				fail(i, row, "configuration modified")
			}
		case "generate":
			a := args[struct {
				Config json.RawMessage `json:"config"`
				Auth   bool            `json:"auth"`
			}](t, row)
			restore := gateway.SetAuthEnabled(a.Auth)
			text, err := convert.GenerateMihomoConfig(ctx, jsonxObject(t, a.Config), convert.GenerateOptions{SkipProviderPreflight: true})
			restore()
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil {
				var want string
				_ = json.Unmarshal(row.Out, &want)
				goDoc, errGo := yamlx.Parse([]byte(text))
				pyDoc, errPy := yamlx.Parse([]byte(want))
				if errGo != nil || errPy != nil || !yamlx.Equal(goDoc, pyDoc) {
					fail(i, row, "generated configuration differs\nGo:\n%s\nPython:\n%s", text, want)
				}
			}
		case "downloads":
			a := args[struct {
				Config json.RawMessage `json:"config"`
				Auth   bool            `json:"auth"`
			}](t, row)
			restore := gateway.SetAuthEnabled(a.Auth)
			downloads, err := convert.GetMihomoRulesetDownloads(ctx, jsonxObject(t, a.Config), "")
			restore()
			if m := errorMismatch(err, row.Err); m != "" {
				fail(i, row, "%s", m)
			} else if err == nil {
				got := make([]any, len(downloads))
				for j, d := range downloads {
					got[j] = d.Object()
				}
				encoded, _ := jsonx.Compact(got)
				if !sameJSON(t, encoded, row.Out) {
					fail(i, row, "Go %s\nPython %s", encoded, row.Out)
				}
			}
		default:
			t.Fatalf("unknown corpus function %q", row.FN)
		}
	}
	t.Logf("%d rows: %v", len(rows), counts)
	if failures > 0 {
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
}
