package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// corpusRow is one line of a testdata/*.jsonl file written by gen.py.
type corpusRow struct {
	Fn     string
	In     any
	Out    any
	InRaw  json.RawMessage
	OutRaw json.RawMessage
}

func decodeAny(raw json.RawMessage) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func loadCorpus(t *testing.T, name string) []corpusRow {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows []corpusRow
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var raw struct {
			Fn  string          `json:"fn"`
			In  json.RawMessage `json:"in"`
			Out json.RawMessage `json:"out"`
		}
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inVal, err := decodeAny(raw.In)
		if err != nil {
			t.Fatalf("%s in: %v", name, err)
		}
		outVal, err := decodeAny(raw.Out)
		if err != nil {
			t.Fatalf("%s out: %v", name, err)
		}
		rows = append(rows, corpusRow{
			Fn:     raw.Fn,
			In:     inVal,
			Out:    outVal,
			InRaw:  bytes.Clone(raw.In),
			OutRaw: bytes.Clone(raw.Out),
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func compact(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonresp.Encode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(b)
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := jsonresp.Encode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

func yamlFromRaw(t *testing.T, raw json.RawMessage) *yamlx.Node {
	t.Helper()
	if len(bytes.TrimSpace(raw)) == 0 {
		return yamlx.Null()
	}
	n, err := yamlx.FromJSON(raw)
	if err != nil {
		t.Fatalf("yamlx.FromJSON(%s): %v", raw, err)
	}
	return n
}

func yamlToRaw(t *testing.T, n *yamlx.Node) json.RawMessage {
	t.Helper()
	if n == nil {
		return json.RawMessage("null")
	}
	b, err := yamlx.ToJSON(n)
	if err != nil {
		t.Fatalf("yamlx.ToJSON: %v", err)
	}
	return b
}

func readSubscriptionYAMLNode(source string, stringKeysOnly bool) (*yamlx.Node, error) {
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	mode := yamlx.RejectNonStringKeys
	if stringKeysOnly {
		mode = yamlx.DiscardNonStringKeys
	}
	if err := yamlx.ProjectJSONKeys(node, mode); err != nil {
		return nil, err
	}
	return node, nil
}

// isErrorRaw reports whether a recorded JSON output is a Python exception, with its text and type.
func isErrorRaw(raw json.RawMessage) (msg, errType string, ok bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", "", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", "", false
	}
	errVal, hasErr := m["error"]
	typeVal, hasType := m["type"]
	if !hasErr || (!hasType && len(m) != 1) {
		return "", "", false
	}
	_ = json.Unmarshal(errVal, &msg)
	if hasType {
		_ = json.Unmarshal(typeVal, &errType)
	}
	return msg, errType, true
}

func decodeConfigRaw(raw json.RawMessage) (*model.Config, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return &model.Config{Profile: &model.Profile{}}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	profileMap := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		profileMap[k] = v
	}
	var profileID string
	if pid, ok := fields["profile_id"]; ok {
		_ = json.Unmarshal(pid, &profileID)
	}
	for _, name := range []string{"profile_id", "system_config", "backup", "agents", "nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		delete(profileMap, name)
	}
	profileBytes, err := json.Marshal(profileMap)
	if err != nil {
		return nil, err
	}
	var profile model.Profile
	if err := jsondecode.Unmarshal(profileBytes, &profile); err != nil {
		return nil, err
	}
	sharedMap := make(map[string]json.RawMessage)
	for _, name := range []string{"nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		if v, ok := fields[name]; ok {
			sharedMap[name] = v
		}
	}
	sharedBytes, err := json.Marshal(sharedMap)
	if err != nil {
		return nil, err
	}
	var shared model.Shared
	if err := jsondecode.Unmarshal(sharedBytes, &shared); err != nil {
		return nil, err
	}
	return &model.Config{
		Profile:       &profile,
		ProfileID:     profileID,
		Nodes:         shared.Nodes,
		Subscriptions: shared.Subscriptions,
		Aggregations:  shared.Aggregations,
		RuleLibrary:   shared.RuleLibrary,
	}, nil
}

func parsedListJSON(t *testing.T, items []ParsedProxy) json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if item.Raw != nil {
			cloned := yamlx.Clone(item.Raw)
			yamlx.Set(cloned, "_raw_object", yamlx.Bool(true))
			b, err := yamlx.ToJSON(cloned)
			if err != nil {
				continue
			}
			out = append(out, b)
		} else {
			b, err := jsonresp.Encode(item.Node)
			if err != nil {
				continue
			}
			out = append(out, b)
		}
	}
	return rawJSON(t, out)
}

func parseObjectJSON(t *testing.T, source string) json.RawMessage {
	t.Helper()
	graph := ParseObject(source)
	if graph == nil {
		return json.RawMessage("null")
	}
	cloned := yamlx.Clone(graph)
	yamlx.Set(cloned, "_raw_object", yamlx.Bool(true))
	return yamlToRaw(t, cloned)
}

func equalJSONRaw(want, got json.RawMessage) bool {
	return testjson.Equal(want, got)
}

// sanitizeNonFiniteJSON replaces Python json.dumps bare NaN/Infinity/-Infinity
// tokens (outside strings) with string placeholders so encoding/json can parse
// frozen Python corpora without altering any valid RFC 8259 JSON bytes.
func sanitizeNonFiniteJSON(src []byte) []byte {
	if !bytes.Contains(src, []byte("Infinity")) && !bytes.Contains(src, []byte("NaN")) {
		return src
	}
	var out bytes.Buffer
	out.Grow(len(src) + 32)
	inStr := false
	for i := 0; i < len(src); {
		c := src[i]
		if inStr {
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			i++
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("-Infinity")) {
			out.WriteString(`"-__Infinity__"`)
			i += len("-Infinity")
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("Infinity")) {
			out.WriteString(`"__Infinity__"`)
			i += len("Infinity")
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("NaN")) {
			out.WriteString(`"__NaN__"`)
			i += len("NaN")
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.Bytes()
}
