package routerdoc

import (
	"bufio"
	"compress/gzip"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/pyip"
)

// TestCorpus replays testdata/corpus.jsonl.gz (testdata/gen_corpus.py, the
// real Python functions on the test server).
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
		fn := row.GetString("fn")
		counts[fn]++
		out, gotErr := run(t, fn, row.Value("in"))
		var got, want any
		if gotErr != nil {
			got = jsonx.Obj("err", []any{gotErr.(interface{ PythonClass() string }).PythonClass(), gotErr.Error()})
		} else {
			got = jsonx.Obj("out", out)
		}
		if e, ok := row.Get("err"); ok {
			want = jsonx.Obj("err", e)
		} else {
			want = jsonx.Obj("out", row.Value("out"))
		}
		g, _ := jsonx.Compact(got)
		w, _ := jsonx.Compact(want)
		if string(g) != string(w) {
			differ++
			if differ <= 25 {
				in, _ := jsonx.Compact(row.Value("in"))
				t.Errorf("%s(%s)\n got  %s\n want %s", fn, in, g, w)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("rows %v, %d differ", counts, differ)
}

type pyErr struct{ class, msg string }

func (e *pyErr) Error() string       { return e.msg }
func (e *pyErr) PythonClass() string { return e.class }

func sightingOf(value any) *Sighting {
	o, ok := value.(*jsonx.Object)
	if !ok || o == nil {
		return nil
	}
	s := NewSighting(o.GetString("ip"))
	s.MAC, s.Hostname, s.Presence, s.Basis, s.RouterID = o.GetString("mac"), o.GetString("hostname"),
		o.GetString("presence"), o.GetString("basis"), o.GetString("router_id")
	for _, src := range o.GetList("sources") {
		s.Sources[src.(string)] = true
	}
	if n, ok := o.Value("last_seen").(jsonx.Number); ok {
		v, _ := n.Int64()
		s.LastSeen = &v
	}
	if f, ok := o.Value("fixed_ip").(*jsonx.Object); ok {
		s.FixedIP = f
	}
	return s
}

func run(t *testing.T, fn string, in any) (any, error) {
	switch fn {
	case "normalize_mac":
		return NormalizeMAC(in), nil
	case "ipv4":
		return IPv4(in), nil
	case "covers":
		o := in.(*jsonx.Object)
		r := NewReading()
		r.Complete = o.GetBool("complete")
		for _, n := range o.GetList("lan") {
			network, _ := pyip.ParseIPv4Network(n.(string), true)
			r.LAN = append(r.LAN, network)
		}
		return r.Covers(o.GetString("ip"))
	case "better":
		o := in.(*jsonx.Object)
		current, candidate := sightingOf(o.Value("current")), sightingOf(o.Value("candidate"))
		result, err := Better(current, candidate)
		if err != nil {
			return nil, err
		}
		which := "candidate"
		if result == current {
			which = "current"
		}
		return jsonx.Obj("which", which, "public", result.Public()), nil
	case "validate_bindings":
		if msg := ValidateBindings(in); msg != "" {
			return nil, &pyErr{"OxidnsSettingsError", msg}
		}
		return nil, nil
	case "routers_of":
		out := []any{}
		for _, r := range RoutersOf(in.(*jsonx.Object)) {
			out = append(out, r)
		}
		return out, nil
	case "find_router":
		o := in.(*jsonx.Object)
		if r := FindRouter(o.GetObject("config"), o.GetString("id")); r != nil {
			return r, nil
		}
		return nil, nil
	}
	t.Fatalf("unknown fn %s", fn)
	return nil, nil
}
