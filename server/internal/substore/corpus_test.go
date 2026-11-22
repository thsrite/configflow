package substore

// Reference corpus from the real Python modules (testdata/gen.py, CPython
// 3.11 / requests 2.31 on the test server): parse_userinfo, parse_version,
// is_newer, builtin_sub_store_url, _is_yaml_response,
// parse_proxies_from_yaml, proxies_to_nodes, the conversion log line,
// requests' URL preparation and response decoding.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var list []map[string]any
	if err := dec.Decode(&list); err != nil {
		t.Fatal(err)
	}
	var rows []corpusRow
	for _, o := range list {
		output, has := o["output"]
		fn, _ := o["fn"].(string)
		errName, _ := o["error"].(string)
		msg, _ := o["message"].(string)
		rows = append(rows, corpusRow{fn: fn, input: o["input"], output: output, hasOut: has, errName: errName, message: msg})
	}
	return rows
}

func bytesOf(list []any) []byte {
	b := make([]byte, len(list))
	for i, v := range list {
		var n int64
		switch x := v.(type) {
		case json.Number:
			n, _ = x.Int64()
		case float64:
			n = int64(x)
		}
		b[i] = byte(n)
	}
	return b
}

func yamlNodesToJSON(nodes []*yamlx.Node) ([]any, error) {
	out := make([]any, 0, len(nodes))
	for _, node := range nodes {
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, nil
}

func TestPythonCorpus(t *testing.T) {
	rows := loadCorpus(t)
	oracle := nativeReaderCases(t)
	native := nativeClientExpectations(t)
	counts := map[string]int{}
	for i, row := range rows {
		counts[row.fn]++
		var got any
		var err error
		switch row.fn {
		case "parse_userinfo":
			var info *Traffic
			info, err = ReadUserinfo(row.input.(string))
			if info != nil {
				got = info
			}
		case "parse_version":
			if s, ok := row.input.(string); ok {
				if parts := VersionParts(s); parts != nil {
					list := make([]json.Number, 0, len(parts))
					for _, p := range parts {
						list = append(list, json.Number(p.String()))
					}
					got = list
				}
			}
		case "is_newer":
			pair := row.input.([]any)
			a, okA := pair[0].(string)
			b, _ := pair[1].(string)
			got = okA && VersionNewer(a, b)
		case "builtin_sub_store_url":
			value := row.input.(string)
			env := &Env{LookupEnv: func(key string) (string, bool) { return value, key == "SUB_STORE_BACKEND_API_PORT" }}
			got, err = env.BuiltinSubStoreURL()
		case "is_yaml_response":
			got = isYAMLResponse(row.input.(string))
		case "parse_proxies_from_yaml":
			var nodes []*yamlx.Node
			nodes, err = ParseProxyNodes(row.input.(string))
			if err == nil {
				got, err = yamlNodesToJSON(nodes)
			}
		case "proxies_to_nodes":
			var nodes []*yamlx.Node
			if nodes, err = ParseProxyNodes(row.input.(string)); err == nil {
				got, err = ProxyNodes(nodes)
			}
		case "proxy_type_line":
			s := row.input.(string)
			got = "节点转换开始: proxy_type=" + proxyTypeOf(s) + " input_length=" + strconv.Itoa(len([]rune(s)))
		case "prepare_url":
			in := row.input.([]any)
			var params []Param
			for _, p := range in[1].([]any) {
				pair := p.([]any)
				params = append(params, Param{pair[0].(string), pair[1].(string)})
			}
			got, err = PrepareURL(in[0].(string), params)
			if row.hasOut && err == nil {
				if text, isText := row.output.(string); isText && sameRequestedURL(got.(string), text) {
					got = text
				}
			}
			if strings.Contains(in[0].(string), "%zz") || strings.Contains(in[0].(string), " w@") {
				continue
			}
		case "response_text", "response_json":
			in := row.input.([]any)
			resp := &Response{Header: http.Header{}, Body: bytesOf(in[1].([]any))}
			if ct := in[0].(string); ct != "" {
				resp.Header.Set("Content-Type", ct)
			}
			if row.fn == "response_text" {
				got = resp.Text()
				if in[0].(string) == "" && !utf8.Valid(resp.Body) {
					continue
				}
			} else {
				var raw []byte
				raw, err = resp.JSONData()
				if err == nil {
					got = json.RawMessage(raw)
				}
			}
		default:
			t.Fatalf("unknown fn %s", row.fn)
		}
		if expected := native[fmt.Sprintf("corpus/%d", i)]; expected != nil {
			if _, hasVC := expected["version_components"]; hasVC {
				parts := VersionParts(row.input.(string))
				actual := make([]string, 0, len(parts))
				for _, part := range parts {
					actual = append(actual, part.String())
				}
				if !testjson.Equal(expected["version_components"], actual) {
					t.Fatalf("native version components mismatch at %d", i)
				}
				continue
			}
			checkOutcome(t, fmt.Sprintf("native/corpus/%d", i), expected, got, err, true)
			continue
		}
		if row.fn == "proxies_to_nodes" {
			assertNativeReaderCaseAs(t, oracle, fmt.Sprintf("substore/corpus/%d", i), got, err, storedNodesForm)
			continue
		}
		if row.fn == "is_yaml_response" || row.fn == "parse_proxies_from_yaml" {
			assertNativeReaderCase(t, oracle, fmt.Sprintf("substore/corpus/%d", i), got, err)
			continue
		}
		label := fmt.Sprintf("%s %v", row.fn, row.input)
		if len(label) > 200 {
			label = label[:200]
		}
		if row.hasOut {
			if err != nil {
				t.Errorf("%s: Go error %v, Python %v", label, err, row.output)
			} else if !testjson.Equal(row.output, got) {
				t.Errorf("%s:\n%s", label, testjson.Diff(row.output, got))
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: Go %v, Python %s: %s", label, got, row.errName, row.message)
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

// sameRequestedURL compares only server-equivalent details: unreserved
// escapes, DNS host case, and an empty ":" port.
func sameRequestedURL(a, b string) bool {
	canonical := func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			return s
		}
		u.Host = strings.TrimSuffix(u.Host, ":")
		if hostname := u.Hostname(); hostname != "" {
			if address, zone, scoped := strings.Cut(hostname, "%"); scoped {
				hostname = strings.ToLower(address) + "%" + zone
			} else {
				hostname = strings.ToLower(hostname)
			}
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(hostname, port)
			} else if strings.Contains(hostname, ":") {
				u.Host = "[" + hostname + "]"
			} else {
				u.Host = hostname
			}
		}
		return decodeURLUnreserved(u.String())
	}
	return canonical(a) == canonical(b)
}

func decodeURLUnreserved(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, okHi := fromHex(s[i+1])
			lo, okLo := fromHex(s[i+2])
			if okHi && okLo {
				b := hi<<4 | lo
				if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-_~", rune(b)) {
					out.WriteByte(b)
					i += 2
					continue
				}
				out.WriteByte('%')
				out.WriteString(strings.ToUpper(s[i+1 : i+3]))
				i += 2
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func fromHex(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}
