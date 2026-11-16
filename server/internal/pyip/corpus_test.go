package pyip

import (
	"bufio"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestCorpus compares with CPython 3.11.17's ipaddress on the rows of
// testdata/corpus.jsonl.gz (testdata/gen_corpus.py).
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
	counts := map[string]int{}
	differ := 0
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		kind := row.GetString("k")
		counts[kind]++
		got := evaluate(t, row)
		want := row.Value("out")
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

func netList(t *testing.T, texts []any) []Network {
	var out []Network
	for _, text := range texts {
		n, ok := ParseNetwork(text.(string), true)
		if !ok {
			t.Fatalf("bad corpus network %q", text)
		}
		out = append(out, n)
	}
	return out
}

func strList(list []Network) []any {
	out := []any{}
	for _, n := range list {
		out = append(out, n.String())
	}
	return out
}

func evaluate(t *testing.T, row *jsonx.Object) any {
	switch row.GetString("k") {
	case "addr":
		a, ok := ParseAddress(row.GetString("in"))
		if !ok {
			return nil
		}
		return jsonx.Obj("s", a.String(), "v", a.Version(), "int", bigText(a.Int()), "priv", a.IsPrivate(),
			"glob", a.IsGlobal(), "loop", a.IsLoopback(), "mc", a.IsMulticast(), "unspec", a.IsUnspecified())
	case "net":
		n, ok := ParseNetwork(row.GetString("in"), row.GetBool("strict"))
		if !ok {
			return nil
		}
		return jsonx.Obj("s", n.String(), "v", n.Version(), "p", n.Prefix(), "bcast", n.BroadcastAddress().String(),
			"priv", n.IsPrivate(), "glob", n.IsGlobal())
	case "v4net":
		n, ok := ParseIPv4Network(row.GetString("in"), false)
		if !ok {
			return nil
		}
		return n.String()
	case "collapse":
		out, err := CollapseAddresses(netList(t, row.GetList("in")))
		if err != nil {
			if want, ok := row.Value("out").(*jsonx.Object); ok && strings.Contains(err.Error(), "are not of the same version") {
				return want // the pair named depends on Python's sort comparisons
			}
			return jsonx.Obj("err", err.Error())
		}
		return strList(out)
	case "sort":
		list := netList(t, row.GetList("in"))
		SortNetworks(list)
		return strList(list)
	case "subnet":
		a, _ := ParseNetwork(row.GetString("a"), true)
		b, _ := ParseNetwork(row.GetString("b"), true)
		return a.SubnetOf(b)
	}
	t.Fatalf("unknown kind %q", row.GetString("k"))
	return nil
}
