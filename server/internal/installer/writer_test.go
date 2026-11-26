package installer

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

var writerOracleValues = sync.OnceValues(func() (map[string]any, error) {
	f, err := os.Open("testdata/n74_writer_expected.json.gz")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	object, err := parseJSONMap(data)
	if err != nil || object == nil {
		return nil, fmt.Errorf("invalid writer oracle fixture")
	}
	return object, nil
})

func writerExpected(fn string, args any) (any, bool, error) {
	values, err := writerOracleValues()
	if err != nil {
		return nil, false, err
	}
	data, err := sortedCompactJSON(map[string]any{"fn": fn, "args": args})
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(data)
	entry := mapObj(values, hex.EncodeToString(sum[:]))
	if entry == nil {
		return nil, false, nil
	}
	if mapBool(entry, "expected_from_legacy") {
		return nil, false, nil
	}
	value, ok := entry["expected"]
	if !ok {
		return nil, false, fmt.Errorf("writer oracle is missing its expected value")
	}
	return cloneJSON(value), true, nil
}

func writerYAMLEqual(want, got string) bool {
	a, err := yamlx.Parse([]byte(want))
	if err != nil {
		return false
	}
	b, err := yamlx.Parse([]byte(got))
	if err != nil {
		return false
	}
	if normalizeComposeSettings(a) != nil || normalizeComposeSettings(b) != nil {
		return false
	}
	return yamlx.Equal(a, b)
}

func writerHashCaseMatches(fn string, args any, output, oldHash string, length int64) (bool, error) {
	if fn == "compose" {
		value, present, err := writerExpected(fn, args)
		if err != nil {
			return false, err
		}
		if present {
			want, ok := value.(string)
			return ok && writerYAMLEqual(want, output), nil
		}
	}
	output, err := historicalEmbeddedJSON(output)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(output))
	return hex.EncodeToString(sum[:]) == oldHash && int64(len(output)) == length, nil
}

func TestComposeWriterPreservesUnicodeSeparators(t *testing.T) {
	for _, ch := range []rune{'\u0085', '\u2028', '\u2029'} {
		for _, method := range []string{"compose", "compose_direct", "compose_compact"} {
			t.Run(fmt.Sprintf("U+%04X/%s", ch, method), func(t *testing.T) {
				data := "/srv/native-a" + string(ch) + "b"
				image := "local.test/agent:latest"
				request := composeParams(t.TempDir(), "install_method", method, "data_dir", data, "image", image)
				result, err := GenerateGuide(request, nil)
				if err != nil {
					t.Fatal(err)
				}
				n, err := yamlx.Parse([]byte(mapStr(result, "compose")))
				if err != nil {
					t.Fatal(err)
				}
				agent := yamlx.Get(yamlx.Get(n, "services"), "agent")
				if value := yamlx.Resolve(yamlx.Get(agent, "image")); value == nil || value.Tag != "!!str" || value.Value != image {
					t.Fatal("native YAML changed the image string")
				}
				volumes := yamlx.Resolve(yamlx.Get(agent, "volumes"))
				if volumes == nil || len(volumes.Content) == 0 {
					t.Fatal("missing data volume")
				}
				volume := yamlx.Resolve(volumes.Content[0])
				if source := yamlx.Resolve(yamlx.Get(volume, "source")); source != nil {
					if source.Value != data+"/agent" {
						t.Fatal("native YAML changed the bind source")
					}
				} else if volume.Value != data+"/agent:/opt/configflow-agent" {
					t.Fatal("native YAML changed the short volume source")
				}
			})
		}
	}
}

func TestComposeWriterPreservesNumericImageStrings(t *testing.T) {
	for _, image := range []string{"0o17", "0X1F", "1e3"} {
		result, err := GenerateGuide(composeParams(t.TempDir(), "install_method", "compose_compact", "image", image), nil)
		if err != nil {
			t.Fatal(err)
		}
		n, err := yamlx.Parse([]byte(mapStr(result, "compose")))
		if err != nil {
			t.Fatal(err)
		}
		for name, service := range yamlx.Pairs(yamlx.Get(n, "services")) {
			value := yamlx.Resolve(yamlx.Get(service, "image"))
			if value == nil || value.Tag != "!!str" || value.Value != image {
				t.Fatalf("service %s image lost its string type", name)
			}
		}
		if !strings.Contains(mapStr(result, "compose"), "&agent_image") {
			t.Fatal("shared image anchor was lost")
		}
	}
}

func TestWriterOracleProvenance(t *testing.T) {
	data, err := os.ReadFile("testdata/n74_writer_manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourceSHA256 map[string]string `json:"source_sha256"`
		Unique       int               `json:"unique_expected"`
		Graphs       int               `json:"graphs"`
		Unknown      int               `json:"unknown"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Unknown != 0 {
		t.Fatalf("invalid writer provenance: %v", err)
	}
	for name, want := range manifest.SourceSHA256 {
		f, err := os.Open("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != want {
			t.Fatalf("old golden source changed: %s: %v", name, err)
		}
	}
	values, err := writerOracleValues()
	if err != nil || len(values) != manifest.Unique {
		t.Fatalf("incomplete writer expectations: %v", err)
	}
	graphIDs := map[string]bool{}
	f, err := os.Open("testdata/n74_writer_graphs.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	d := json.NewDecoder(gz)
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("invalid graph fixture: %v", err)
	}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		id, ok := token.(string)
		if !ok || graphIDs[id] {
			t.Fatal("invalid/duplicate graph identity")
		}
		var record struct {
			Nodes json.RawMessage `json:"nodes"`
			YAML  string          `json:"yaml"`
		}
		if err := d.Decode(&record); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(record.Nodes)
		if hex.EncodeToString(sum[:]) != id {
			t.Fatal("typed Python graph digest changed")
		}
		var reference yamlxtest.WriterReference
		if err := json.Unmarshal(record.Nodes, &reference.Nodes); err != nil {
			t.Fatal(err)
		}
		n, err := reference.Document()
		if err == nil {
			n, err = yamlbridge.Encode(n)
		}
		if err != nil {
			t.Fatal(err)
		}
		out, err := yamlx.Marshal(n)
		if err != nil || string(out) != record.YAML {
			t.Fatalf("injected writer bytes cannot be reproduced: %s: %v", id, err)
		}
		graphIDs[id] = true
	}
	if len(graphIDs) != manifest.Graphs {
		t.Fatalf("graph count %d, want %d", len(graphIDs), manifest.Graphs)
	}
	for id, value := range values {
		entry := value.(map[string]any)
		input, err := sortedCompactJSON(map[string]any{"fn": entry["fn"], "args": entry["args"]})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(input)
		if hex.EncodeToString(sum[:]) != id || len(mapList(entry, "sources")) == 0 || len(mapList(entry, "categories")) == 0 {
			t.Fatalf("invalid source/case identity: %s", id)
		}
		for _, field := range []string{"calls", "legacy_calls"} {
			for _, call := range mapList(entry, field) {
				if !graphIDs[mapStr(call.(map[string]any), "graph")] {
					t.Fatal("writer call has no recorded typed graph")
				}
			}
		}
		if expected, ok := entry["expected"]; ok {
			data, err := sortedCompactJSON(expected)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != mapStr(entry, "native_result_sha256") {
				t.Fatal("complete native Python expectation changed")
			}
		} else if categories := mapList(entry, "categories"); !mapBool(entry, "expected_from_legacy") || len(categories) != 1 || categories[0] != "layout" {
			t.Fatal("a semantic change cannot use an unchanged legacy expectation")
		}
	}
}

func TestWriterComparisonPreservesScalarTags(t *testing.T) {
	for _, tagged := range []string{"!!timestamp 2001-01-01", "!!binary YQ==", "!!float .inf", "!custom text", "!!int 100000000000000000000"} {
		t.Run(tagged, func(t *testing.T) {
			n, err := yamlx.Parse([]byte("value: " + tagged + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			plain := "value: " + fmt.Sprintf("%q", yamlx.Get(n, "value").Value) + "\n"
			if nativeOutcomeEqual("value: "+tagged+"\n", plain, "compose") {
				t.Fatal("writer comparison erased a scalar tag")
			}
			before := map[string]any{"content_base64": testComposeArchive(t, "value: "+tagged+"\n", "echo synthetic\n", 0o100600, false)}
			after := map[string]any{"content_base64": testComposeArchive(t, plain, "echo synthetic\n", 0o100600, false)}
			if nativeOutcomeEqual(before, after, "") {
				t.Fatal("archive writer comparison erased a scalar tag")
			}
		})
	}
}

// historicalEmbeddedJSON normalizes only the JSON heredocs for comparison with
// the frozen byte hashes. Every shell byte outside these delimiters is retained.
func historicalEmbeddedJSON(text string) (string, error) {
	for _, marker := range []string{"CONFIGFLOW_JSON_EOF", "CONFIGFLOW_MIHOMO_JSON_EOF", "CONFIGFLOW_OXIDNS_JSON_EOF"} {
		opening := "<<'" + marker + "'\n"
		start := 0
		for {
			offset := strings.Index(text[start:], opening)
			if offset < 0 {
				break
			}
			first := start + offset + len(opening)
			end := strings.Index(text[first:], "\n"+marker+"\n")
			if end < 0 {
				return "", fmt.Errorf("unterminated config heredoc")
			}
			last := first + end
			data, err := formatOrderedJSON([]byte(text[first:last]), true, false, true)
			if err != nil {
				return "", err
			}
			text = text[:first] + string(data) + text[last:]
			start = first + len(data) + len(marker) + 2
		}
	}
	return text, nil
}

func historicalBootstrapSettings(text string) (string, error) {
	const prefix = "settings = json.loads("
	start := 0
	for {
		found := strings.Index(text[start:], prefix)
		if found < 0 {
			break
		}
		first := start + found + len(prefix)
		end := strings.Index(text[first:], ")\npath = ")
		if end < 0 {
			start = first
			continue
		}
		last := first + end
		literal := text[first:last]
		if len(literal) < 2 || (literal[0] != '\'' && literal[0] != '"') || literal[len(literal)-1] != literal[0] {
			return "", fmt.Errorf("invalid bootstrap settings literal")
		}
		quote := literal[0]
		source := literal[1 : len(literal)-1]
		var raw strings.Builder
		for source != "" {
			r, _, tail, err := strconv.UnquoteChar(source, quote)
			if err != nil {
				return "", err
			}
			raw.WriteRune(r)
			source = tail
		}
		encoded, err := formatOrderedJSON([]byte(raw.String()), false, true, true)
		if err != nil {
			return "", err
		}
		normalized := pyASCIIStringRepr(string(encoded))
		text = text[:first] + normalized + text[last:]
		start = first + len(normalized)
	}
	return text, nil
}

func normalizeComposeSettings(root *yamlx.Node) error {
	for _, service := range yamlx.Pairs(yamlx.Get(root, "services")) {
		environment := yamlx.Get(service, "environment")
		value := yamlx.Get(environment, "CONFIGFLOW_COMPOSE_INSTALL")
		if value == nil {
			continue
		}
		raw, err := formatOrderedJSON([]byte(nodeText(value)), false, false, true)
		if err != nil {
			return err
		}
		yamlx.Set(environment, "CONFIGFLOW_COMPOSE_INSTALL", yamlx.Str(string(raw)))
	}
	return nil
}
