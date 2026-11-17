package installer

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

var writerOracleValues = sync.OnceValues(func() (*jsonx.Object, error) {
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
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, fmt.Errorf("invalid writer oracle fixture")
	}
	return object, nil
})

func writerExpected(fn string, args any) (any, bool, error) {
	values, err := writerOracleValues()
	if err != nil {
		return nil, false, err
	}
	data, err := jsonx.Sorted(jsonx.Obj("fn", fn, "args", args))
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(data)
	entry := values.GetObject(hex.EncodeToString(sum[:]))
	if entry == nil {
		return nil, false, nil
	}
	if entry.GetBool("expected_from_legacy") {
		return nil, false, nil
	}
	value, ok := entry.Get("expected")
	if !ok {
		return nil, false, fmt.Errorf("writer oracle is missing its expected value")
	}
	return jsonx.DeepCopy(value), true, nil
}

func writerYAMLEqual(want, got string) bool {
	a, err := yamlx.Parse([]byte(want))
	if err != nil {
		return false
	}
	b, err := yamlx.Parse([]byte(got))
	return err == nil && yamlx.Equal(a, b)
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
				n, err := yamlx.Parse([]byte(result.GetString("compose")))
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
		n, err := yamlx.Parse([]byte(result.GetString("compose")))
		if err != nil {
			t.Fatal(err)
		}
		for name, service := range yamlx.Pairs(yamlx.Get(n, "services")) {
			value := yamlx.Resolve(yamlx.Get(service, "image"))
			if value == nil || value.Tag != "!!str" || value.Value != image {
				t.Fatalf("service %s image lost its string type", name)
			}
		}
		if !strings.Contains(result.GetString("compose"), "&agent_image") {
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
	if err != nil || values.Len() != manifest.Unique {
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
	for id, value := range values.All() {
		entry := value.(*jsonx.Object)
		input, err := jsonx.Sorted(jsonx.Obj("fn", entry.Value("fn"), "args", entry.Value("args")))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(input)
		if hex.EncodeToString(sum[:]) != id || len(entry.GetList("sources")) == 0 || len(entry.GetList("categories")) == 0 {
			t.Fatalf("invalid source/case identity: %s", id)
		}
		for _, field := range []string{"calls", "legacy_calls"} {
			for _, call := range entry.GetList(field) {
				if !graphIDs[call.(*jsonx.Object).GetString("graph")] {
					t.Fatal("writer call has no recorded typed graph")
				}
			}
		}
		if expected, ok := entry.Get("expected"); ok {
			data, err := jsonx.Sorted(expected)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != entry.GetString("native_result_sha256") {
				t.Fatal("complete native Python expectation changed")
			}
		} else if categories := entry.GetList("categories"); !entry.GetBool("expected_from_legacy") || len(categories) != 1 || categories[0] != "layout" {
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
			before := jsonx.Obj("content_base64", testComposeArchive(t, "value: "+tagged+"\n", "echo synthetic\n", 0o100600, false))
			after := jsonx.Obj("content_base64", testComposeArchive(t, plain, "echo synthetic\n", 0o100600, false))
			if nativeOutcomeEqual(before, after, "") {
				t.Fatal("archive writer comparison erased a scalar tag")
			}
		})
	}
}
