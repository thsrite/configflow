package netipx

import (
	"bufio"
	"compress/gzip"
	"net/netip"
	"os"
	"testing"

	"configflow/internal/jsonx"
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
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		kind := row.GetString("k")
		counts[kind]++
		got, want := evaluate(t, row)
		if !jsonx.Equal(got, want) {
			differ++
			if differ <= 30 {
				g, _ := jsonx.Compact(got)
				w, _ := jsonx.Compact(want)
				in, _ := jsonx.Compact(row)
				t.Errorf("%s\n got  %s\n want %s", in, g, w)
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
func evaluate(t *testing.T, row *jsonx.Object) (any, any) {
	want := row.Value("out")
	switch row.GetString("k") {
	case "addr":
		var w any
		if o, ok := want.(*jsonx.Object); ok {
			w = jsonx.Obj("s", o.Value("s"), "v", o.Value("v"))
			if o.GetInt("v") == 4 {
				w.(*jsonx.Object).Update(jsonx.Obj("loop", o.Value("loop"), "mc", o.Value("mc"), "unspec", o.Value("unspec")))
			}
		}
		a, err := netip.ParseAddr(row.GetString("in"))
		if err != nil {
			return nil, w
		}
		got := jsonx.Obj("s", a.String(), "v", family(a))
		if a.Is4() {
			got.Update(jsonx.Obj("loop", a.IsLoopback(), "mc", a.IsMulticast(), "unspec", a.IsUnspecified()))
		}
		return got, w
	case "net":
		n, ok := ParseNetwork(row.GetString("in"), row.GetBool("strict"))
		if !ok {
			return nil, want
		}
		return jsonx.Obj("s", n.String(), "v", family(n.Addr()), "p", n.Bits(), "bcast", Broadcast(n).String(),
			"priv", IsPrivate(n), "glob", IsGlobal(n)), want
	case "v4net":
		n, ok := ParseNetwork4(row.GetString("in"), false)
		if !ok {
			return nil, want
		}
		return n.String(), want
	case "collapse":
		out, err := Collapse(parseList(t, row.GetList("in")))
		if err != nil {
			if _, ok := want.(*jsonx.Object); ok {
				return want, want // rejected on both sides (TypeError text not chased)
			}
			return jsonx.Obj("err", err.Error()), want
		}
		return texts(out), want
	case "sort":
		list := parseList(t, row.GetList("in"))
		SortPrefixes(list)
		return texts(list), want
	case "subnet":
		a, _ := ParseNetwork(row.GetString("a"), true)
		b, _ := ParseNetwork(row.GetString("b"), true)
		return b.Bits() <= a.Bits() && b.Contains(a.Addr()), want
	}
	t.Fatalf("unknown kind %q", row.GetString("k"))
	return nil, nil
}
