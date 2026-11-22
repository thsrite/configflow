package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

type nativeDialerCase struct {
	InputSHA string          `json:"input_sha256"`
	Reason   string          `json:"reason"`
	Expected json.RawMessage `json:"expected"`
}

func nativeDialersExpectations(t *testing.T) map[string]nativeDialerCase {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_dialers_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources   map[string]string           `json:"sources"`
		LoaderSHA string                      `json:"native_loader_sha256"`
		Cases     map[string]nativeDialerCase `json:"cases"`
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
			t.Fatalf("dialers source drift %s", name)
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
		t.Fatal("dialer loader archive drift")
	}
	for key, entry := range manifest.Cases {
		if entry.Reason == "" {
			t.Fatalf("unclassified dialers boundary %s", key)
		}
		archive, indexText, ok := strings.Cut(key, "/")
		index, err := strconv.Atoi(indexText)
		if !ok || err != nil || index < 0 || index >= len(lines[archive]) {
			t.Fatal("dialers fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[archive][index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("dialers input drift %s", key)
		}
	}
	return manifest.Cases
}

func TestN89DialerNativeErrorReferences(t *testing.T) {
	cases := nativeDialersExpectations(t)
	rows := map[string][]corpusRow{"dialers": loadCorpus(t, "dialers.jsonl.gz"), "review_c10": loadCorpus(t, "review_c10.jsonl.gz")}
	for key, entry := range cases {
		t.Run(key, func(t *testing.T) {
			archive, indexText, _ := strings.Cut(key, "/")
			index, _ := strconv.Atoi(indexText)
			var expected struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(entry.Expected, &expected); err != nil {
				t.Fatal(err)
			}
			_, err := ExtractDialers(rows[archive][index].In.(string))
			var shape *SubscriptionShapeError
			if !errors.As(err, &shape) || err.Error() != expected.Error {
				t.Fatalf("native error lost type/field: %T %v, want %s", err, err, expected.Error)
			}
		})
	}
}
func TestN89AirportTypeCompatibilityAndNumericNames(t *testing.T) {
	for _, value := range []*yamlx.Node{nil, yamlx.Null(), yamlx.Int(5), yamlx.Bool(true), yamlx.Map(), yamlx.Str("relay"), yamlx.Str("unknown")} {
		if got := airportGroupType(value); got != "select" {
			t.Fatalf("airport fallback %q", got)
		}
	}
	for _, name := range groupTypes {
		if got := airportGroupType(yamlx.Str(name)); got != name {
			t.Fatalf("supported airport kind %q", got)
		}
	}
	record, err := ExtractDialers("proxies: [{name: 5}, {name: node, dialer-proxy: R}]\nproxy-groups: [{name: R, type: 5, proxies: [5]}]\n")
	if err != nil || record == nil {
		t.Fatalf("numeric name source %v", err)
	}
	target, ok := record.Targets.Get("R")
	if !ok || target.Type != "select" || len(target.Members) != 1 || target.Members[0] != "5" {
		t.Fatalf("numeric name was lost %#v", target)
	}
	ops := sharedDialers()
	if ops.Validate == nil || ops.NodeDialer == nil || ops.Members == nil || ops.Resolve == nil || ops.Filter == nil {
		t.Fatal("production shared dialer wiring incomplete")
	}
	var group model.ProxyGroup
	if err := json.Unmarshal([]byte(`{"id":"group","name":"Group","type":5}`), &group); err != nil {
		t.Fatal(err)
	}
	// O18 T1c: a stored number type reads as its text ("5"), no longer kept
	// as the R5 value that failed with "type 必须是字符串" (accepted lenient read).
	if group.Type != "5" {
		t.Fatalf("stored numeric type read as %q", group.Type)
	}
	if err := ops.Validate(&model.Config{Profile: &model.Profile{ProxyGroups: []model.ProxyGroup{group}}}); err != nil && strings.Contains(err.Error(), "type 必须是字符串") {
		t.Fatalf("lenient stored type still rejected as R5 value: %v", err)
	}
}
func TestN89DialerSubgroupTraversalIsBounded(t *testing.T) {
	var source strings.Builder
	source.WriteString("proxies: [{name: leaf}, {name: start, dialer-proxy: g29}]\nproxy-groups:\n- {name: g0, proxies: [leaf]}\n")
	for i := 1; i < 30; i++ {
		fmt.Fprintf(&source, "- {name: g%d, proxies: [g%d, g%d]}\n", i, i-1, i-1)
	}
	record, err := ExtractDialers(source.String())
	if err != nil || record == nil {
		t.Fatal(err)
	}
	group, ok := record.Targets.Get("g29")
	if !ok || len(group.Members) != 1 || group.Members[0] != "leaf" {
		t.Fatalf("bounded traversal lost leaf %#v", group)
	}
}
func TestN89AdaptKeepsOpenParameterKeys(t *testing.T) {
	proxy, err := yamlx.Parse([]byte("name: Node\ndialer-proxy: R\nopaque: {1: value}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var record DialersData
	if err := json.Unmarshal([]byte(`{"targets":{"R":{"type":"select","members":["relay"]}},"unresolved":[]}`), &record); err != nil {
		t.Fatal(err)
	}
	main := yamlx.Map(yamlx.Str("proxy-groups"), yamlx.Seq(yamlx.Map(yamlx.Str("name"), yamlx.Str("Air/R"))))
	selected, err := AdaptProxies("Air", []*yamlx.Node{proxy}, main, &record, true)
	if err != nil || len(selected) != 1 {
		t.Fatalf("open proxy rejected %v", err)
	}
	if !yamlx.Equal(yamlx.Get(selected[0], "opaque"), yamlx.Get(proxy, "opaque")) {
		t.Fatal("open parameter keys changed")
	}
	if !yamlx.Equal(yamlx.Get(proxy, "dialer-proxy"), yamlx.Str("R")) || !yamlx.Equal(yamlx.Get(selected[0], "dialer-proxy"), yamlx.Str("Air/R")) {
		t.Fatal("source mutation or missing replacement")
	}
}
