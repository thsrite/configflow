package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/jsonresp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

type nativeChainCase struct {
	InputSHA string          `json:"input_sha256"`
	Classes  []string        `json:"classes"`
	Expected json.RawMessage `json:"expected"`
}

func nativeChainExpectations(t *testing.T) map[string]nativeChainCase {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_chain_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources map[string]string          `json:"sources"`
		N7SHA   string                     `json:"n7_reference_sha256"`
		Cases   map[string]nativeChainCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	lines := map[string][]string{}
	for name, want := range manifest.Sources {
		filename := "testdata/" + name + ".jsonl.gz"
		if name == "golden" {
			filename = "../repo/testdata/compat_golden.json"
		}
		if name == "drift" {
			filename = "../convert/testdata/corpus.jsonl.gz"
		}
		source, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("chain source drift %s", name)
		}
		if name == "golden" {
			var root struct {
				Cases []json.RawMessage `json:"cases"`
			}
			if err := json.Unmarshal(sanitizeNonFiniteJSON(source), &root); err != nil {
				t.Fatal(err)
			}
			for index, entry := range root.Cases {
				if _, changed := manifest.Cases[fmt.Sprintf("golden/%d", index)]; !changed {
					lines[name] = append(lines[name], "")
					continue
				}
				canonical, err := jsonresp.Canonical(entry)
				if err != nil {
					t.Fatal(err)
				}
				lines[name] = append(lines[name], string(canonical))
			}
			continue
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
	reference, err := os.ReadFile("testdata/n7_reader_expected.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(reference)
	if hex.EncodeToString(sum[:]) != manifest.N7SHA {
		t.Fatal("chain N7 reference drift")
	}
	for key, entry := range manifest.Cases {
		if len(entry.Classes) == 0 {
			t.Fatalf("unclassified chain boundary %s", key)
		}
		archive, indexText, ok := strings.Cut(key, "/")
		index, err := strconv.Atoi(indexText)
		if !ok || err != nil || index < 0 || index >= len(lines[archive]) {
			t.Fatal("chain fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[archive][index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("chain input drift %s", key)
		}
	}
	return manifest.Cases
}
