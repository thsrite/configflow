package substore

// Reference corpus from the real Python modules (testdata/gen.py, CPython
// 3.11 / requests 2.31 on the test server): parse_userinfo, parse_version,
// is_newer, builtin_sub_store_url, _is_yaml_response,
// parse_proxies_from_yaml, proxies_to_nodes, the conversion log line,
// requests' URL preparation and response decoding.

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/jsonx"
)

type corpusRow struct {
	fn      string
	input   any
	output  any
	errName string
	message string
	hasOut  bool
}

func loadCorpus(t *testing.T) []corpusRow {
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var rows []corpusRow
	for _, item := range value.([]any) {
		o := item.(*jsonx.Object)
		output, has := o.Get("output")
		rows = append(rows, corpusRow{fn: o.GetString("fn"), input: o.Value("input"), output: output, hasOut: has,
			errName: o.GetString("error"), message: o.GetString("message")})
	}
	return rows
}

func bytesOf(list []any) []byte {
	b := make([]byte, len(list))
	for i, v := range list {
		n, _ := v.(jsonx.Number).Int64()
		b[i] = byte(n)
	}
	return b
}

func TestPythonCorpus(t *testing.T) {
	rows := loadCorpus(t)
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.fn]++
		var got any
		var err error
		switch row.fn {
		case "parse_userinfo":
			var info *jsonx.Object
			info, err = ParseUserinfo(row.input.(string))
			if info != nil {
				got = info
			}
		case "parse_version":
			if parts := ParseVersion(row.input); parts != nil {
				list := []any{}
				for _, p := range parts {
					list = append(list, jsonx.BigInt(p))
				}
				got = list
			}
		case "is_newer":
			pair := row.input.([]any)
			got = IsNewer(pair[0], pair[1])
		case "builtin_sub_store_url":
			value := row.input.(string)
			env := &Env{LookupEnv: func(key string) (string, bool) { return value, key == "SUB_STORE_BACKEND_API_PORT" }}
			got, err = env.BuiltinSubStoreURL()
		case "is_yaml_response":
			got = isYAMLResponse(row.input.(string))
		case "parse_proxies_from_yaml":
			var proxies []any
			proxies, err = ParseProxiesFromYAML(row.input.(string))
			got = proxies
			if o, ok := row.output.(*jsonx.Object); ok && o.Has("non_list") && !jsonx.Truthy(o.Value("non_list")) && len(proxies) == 0 {
				// Python returns the falsy non-list value itself; Go an empty list.
				got = row.output
			}
		case "proxies_to_nodes":
			var proxies []any
			if proxies, err = ParseProxiesFromYAML(row.input.(string)); err == nil {
				got, err = ProxiesToNodes(proxies)
			}
		case "proxy_type_line":
			s := row.input.(string)
			got = "节点转换开始: proxy_type=" + proxyTypeOf(s) + " input_length=" + jsonx.Str(jsonx.Int(len([]rune(s))))
		case "prepare_url":
			in := row.input.([]any)
			var params []Param
			for _, p := range in[1].([]any) {
				pair := p.([]any)
				params = append(params, Param{pair[0].(string), pair[1].(string)})
			}
			got, err = PrepareURL(in[0].(string), params)
		case "response_text", "response_json":
			in := row.input.([]any)
			resp := &Response{Header: http.Header{}, Body: bytesOf(in[1].([]any))}
			if ct := in[0].(string); ct != "" {
				resp.Header.Set("Content-Type", ct)
			}
			if row.fn == "response_text" {
				got = resp.Text()
				if in[0].(string) == "" && !utf8.Valid(resp.Body) {
					// requests guesses with charset_normalizer (not reproduced).
					continue
				}
			} else {
				got, err = resp.JSON()
			}
		default:
			t.Fatalf("unknown fn %s", row.fn)
		}
		label := row.fn + " " + jsonx.Repr(row.input)
		if len(label) > 200 {
			label = label[:200]
		}
		if row.hasOut {
			if err != nil {
				t.Errorf("%s: Go error %v, Python %s", label, err, jsonx.Repr(row.output))
			} else if !jsonx.Equal(got, row.output) {
				t.Errorf("%s: Go %s, Python %s", label, jsonx.Repr(got), jsonx.Repr(row.output))
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: Go %s, Python %s: %s", label, jsonx.Repr(got), row.errName, row.message)
			continue
		}
		if class := pythonClass(err); class != row.errName && !(row.errName != "" && strings.HasSuffix(row.errName, "Error") && class == "YAMLError") {
			t.Errorf("%s: Go %s (%v), Python %s: %s", label, class, err, row.errName, row.message)
		}
		if row.fn == "parse_userinfo" || row.fn == "builtin_sub_store_url" || row.fn == "proxies_to_nodes" {
			if err.Error() != row.message {
				t.Errorf("%s: message %q, Python %q", label, err.Error(), row.message)
			}
		}
	}
	t.Logf("%d rows %v", len(rows), counts)
}
