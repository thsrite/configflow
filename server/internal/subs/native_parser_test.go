package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

type nativeParserCase struct {
	InputSHA string          `json:"input_sha256"`
	Classes  []string        `json:"classes"`
	Output   json.RawMessage `json:"output"`
	Reject   bool            `json:"reject"`
}

func nativeParserExpectations(t *testing.T) map[string]nativeParserCase {
	t.Helper()
	zipped, err := os.ReadFile("testdata/n89_parser_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(z)
	z.Close()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources   map[string]string           `json:"sources"`
		LoaderSHA string                      `json:"native_loader_sha256"`
		Cases     map[string]nativeParserCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	lines := map[string][]string{}
	for name, want := range manifest.Sources {
		source, err := os.ReadFile("testdata/" + name + ".jsonl.gz")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("parser source drift %s", name)
		}
		reader, err := gzip.NewReader(bytes.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			lines[name] = append(lines[name], scanner.Text())
		}
		reader.Close()
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile("testdata/n7_native_values.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != manifest.LoaderSHA {
		t.Fatal("parser native loader archive drift")
	}
	for key, entry := range manifest.Cases {
		if len(entry.Classes) == 0 {
			t.Fatalf("unclassified parser boundary %s", key)
		}
		archive, indexText, ok := strings.Cut(key, "/")
		index, err := strconv.Atoi(indexText)
		if !ok || err != nil || index < 0 || index >= len(lines[archive]) {
			t.Fatal("parser fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[archive][index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("parser input drift %s", key)
		}
	}
	return manifest.Cases
}

func TestN89NativeParserRecords(t *testing.T) {
	parsed := ParseList(`trojan://password@example.com:443#A`)
	if len(parsed) != 1 || parsed[0].Raw != nil || parsed[0].Node.Name != "A" || parsed[0].Node.Port != 443 {
		t.Fatalf("parsed %#v", parsed)
	}
	raw := ParseList(`{name: 5, type: ss, server: 123, port: "443", password: 123, future: [true, 9007199254740993]}`)
	if len(raw) != 1 || raw[0].Raw == nil || raw[0].Node.Name != "5" || raw[0].Node.Server != "123" || raw[0].Node.Port != 443 {
		t.Fatalf("raw %#v", raw)
	}
	field := yamlx.Get(raw[0].Raw, "password")
	if field == nil || field.ShortTag() != "!!int" || field.Value != "123" {
		t.Fatal("open password normalized")
	}
	data, err := model.Marshal(parsed[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"name":"A","type":"trojan","server":"example.com","port":443,"params":{"password":"password","sni":"","skip-cert-verify":false}}` {
		t.Fatalf("node boundary %s", data)
	}
}

func TestN89VMessJSONDuplicateNamesKeepLast(t *testing.T) {
	values := ParseList("vmess://eyJwcyI6ICJhIiwgInBzIjogImIifQ==")
	if len(values) != 1 || values[0].Node.Name != "b" {
		t.Fatalf("last JSON member lost %#v", values)
	}
}
func TestN89RawProxyNumericTokensRemainLossless(t *testing.T) {
	values := ParseList(`{"name":"x","port":1e400,"future":9007199254740993123456789}`)
	if len(values) != 1 || values[0].Raw == nil {
		t.Fatal("raw object dropped")
	}
	data, err := yamlx.ToJSON(values[0].Raw)
	if err != nil || string(data) != `{"name":"x","port":1e400,"future":9007199254740993123456789}` {
		t.Fatalf("tokens %s %v", data, err)
	}
	if values[0].Node.Port != 0 || !model.IssueUnder(model.Issues(&values[0].Node), "port") {
		t.Fatal("typed overflow lost its issue")
	}
	values = ParseList(`{"name":"x","v":NaN}`)
	if len(values) != 1 || values[0].Raw == nil {
		t.Fatal("native YAML plain token lost")
	}
	data, err = yamlx.ToJSON(values[0].Raw)
	if err != nil || string(data) != `{"name":"x","v":"NaN"}` {
		t.Fatalf("plain token %s %v", data, err)
	}
}

func TestN89VMessNullIsNotAnObject(t *testing.T) {
	if parsed := ParseList("vmess://bnVsbA=="); len(parsed) != 0 {
		t.Fatal("null became a node", parsed)
	}
}
