package netipx

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/netip"
	"os"
	"testing"

	"configflow/internal/testjson"
)

// TestCorpus compares with CPython 3.11.17's ipaddress (testdata/gen_corpus.py)
// structurally: accepted or rejected, canonical text, family, broadcast,
// is_private/is_global of networks, IPv4 loopback/multicast/unspecified,
// collapse and sort results. Python's integer and IPv6-address
// classification fields are not used by the router code and are skipped.
func TestCorpus(t *testing.T) {
	file, err := os.Open("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	counts, differ := map[string]int{}, 0
	for scanner.Scan() {
		raw := scanner.Bytes()
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			t.Fatal(err)
		}
		kind, _ := row["k"].(string)
		counts[kind]++
		got, want := evaluate(t, row)
		if !testjson.Equal(got, want) {
			differ++
			if differ <= 30 {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Errorf("%s\n got  %s\n want %s", raw, g, w)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("rows per kind %v, %d differ", counts, differ)
}

func parseList(t *testing.T, texts []any) []netip.Prefix {
	var out []netip.Prefix
	for _, text := range texts {
		n, ok := ParseNetwork(text.(string), true)
		if !ok {
			t.Fatalf("bad corpus network %q", text)
		}
		out = append(out, n)
	}
	return out
}

func texts(list []netip.Prefix) []any {
	out := []any{}
	for _, n := range list {
		out = append(out, n.String())
	}
	return out
}

func family(a netip.Addr) int {
	if a.Is4() {
		return 4
	}
	return 6
}

// evaluate returns (got, want) for one row.
func evaluate(t *testing.T, row map[string]any) (any, any) {
	want := row["out"]
	kind, _ := row["k"].(string)
	switch kind {
	case "addr":
		var w any
		if o, ok := want.(map[string]any); ok {
			wm := map[string]any{"s": o["s"], "v": o["v"]}
			if v, ok := o["v"].(json.Number); ok && v == "4" {
				wm["loop"] = o["loop"]
				wm["mc"] = o["mc"]
				wm["unspec"] = o["unspec"]
			}
			w = wm
		}
		in, _ := row["in"].(string)
		a, err := netip.ParseAddr(in)
		if err != nil {
			return nil, w
		}
		got := map[string]any{"s": a.String(), "v": family(a)}
		if a.Is4() {
			got["loop"] = a.IsLoopback()
			got["mc"] = a.IsMulticast()
			got["unspec"] = a.IsUnspecified()
		}
		return got, w
	case "net":
		in, _ := row["in"].(string)
		strict, _ := row["strict"].(bool)
		n, ok := ParseNetwork(in, strict)
		if !ok {
			return nil, want
		}
		return map[string]any{"s": n.String(), "v": family(n.Addr()), "p": n.Bits(), "bcast": Broadcast(n).String(),
			"priv": IsPrivate(n), "glob": IsGlobal(n)}, want
	case "v4net":
		in, _ := row["in"].(string)
		n, ok := ParseNetwork4(in, false)
		if !ok {
			return nil, want
		}
		return n.String(), want
	case "collapse":
		in, _ := row["in"].([]any)
		out, err := Collapse(parseList(t, in))
		if err != nil {
			if _, ok := want.(map[string]any); ok {
				return want, want // rejected on both sides (TypeError text not chased)
			}
			return map[string]any{"err": err.Error()}, want
		}
		return texts(out), want
	case "sort":
		in, _ := row["in"].([]any)
		list := parseList(t, in)
		SortPrefixes(list)
		return texts(list), want
	case "subnet":
		aStr, _ := row["a"].(string)
		bStr, _ := row["b"].(string)
		a, _ := ParseNetwork(aStr, true)
		b, _ := ParseNetwork(bStr, true)
		return b.Bits() <= a.Bits() && b.Contains(a.Addr()), want
	}
	t.Fatalf("unknown kind %q", kind)
	return nil, nil
}
