package yamlx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// marshalReference is Marshal as it was before O10 R9 planned the output
// preparation: copy the whole graph, prepare it in place, validate, write.
func marshalReference(v any) ([]byte, error) {
	n, err := encode(v)
	if err != nil {
		return nil, err
	}
	if err := checkMarshalBindings(n); err != nil {
		return nil, err
	}
	n = copyGraph(n, map[*Node]*Node{})
	var prepare func(n *Node, seen map[*Node]bool, flow bool)
	prepare = func(n *Node, seen map[*Node]bool, flow bool) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		prepareNode(n, flow)
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if key := n.Content[i]; key.Kind == yaml.SequenceNode || key.Kind == yaml.MappingNode {
					key.HeadComment = joinComment(key.HeadComment, key.LineComment)
					key.LineComment = ""
				}
			}
		}
		for _, c := range n.Content {
			prepare(c, seen, flow || n.Style&yaml.FlowStyle != 0)
		}
		prepare(n.Alias, seen, flow)
	}
	prepare(n, map[*Node]bool{}, false)
	if err := checkGraph(n, map[*Node]bool{}, map[*Node]bool{}); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(n); err != nil {
		return nil, err
	}
	if err := e.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func checkMarshalPlan(t *testing.T, src []byte) {
	t.Helper()
	n, err := Parse(src)
	if err != nil {
		return
	}
	before := CopyGraph(n)
	want, wantErr := marshalReference(n)
	got, gotErr := Marshal(n)
	if (wantErr == nil) != (gotErr == nil) || !bytes.Equal(got, want) {
		t.Fatalf("%q:\nMarshal  %q (%v)\nreference %q (%v)", src, got, gotErr, want, wantErr)
	}
	// The caller's graph is never changed.
	after, _ := marshalReference(n)
	if reference, _ := marshalReference(before); !bytes.Equal(after, reference) {
		t.Fatalf("%q: Marshal changed its input", src)
	}
}

func marshalPlanInputs(t testing.TB) [][]byte {
	t.Helper()
	var inputs [][]byte
	for _, name := range []string{"testdata/corpus.jsonl", "testdata/review.jsonl"} {
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 1<<24)
		for scanner.Scan() {
			var row struct {
				Input string `json:"input"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) == nil && row.Input != "" {
				inputs = append(inputs, []byte(row.Input))
			}
		}
		file.Close()
	}
	return append(inputs,
		[]byte("a: &x {b: 1}\nc: *x\n"), []byte("? [a, b] # note\n: 1\n"), []byte("<<: {a: 1}\nx: '<<'\n"),
		[]byte("folded: >\n  a\n    b\n"), []byte("~: 1\n'': 2\n? !!null ''\n: 3\n"), []byte("t: !!timestamp 2001-12-14 21:59:43.10 -5\nf: [!!timestamp 2001-12-14 21:59:43.10 -5]\n"),
		[]byte("n: yes\nm: 1:20\nk: no:thing\nproxies:\n  - {name: a, port: 443, udp: true}\n"))
}

// O10 R9: planning the output preparation writes what preparing a copy in
// place wrote, and leaves the input graph unchanged.
func TestMarshalPlanMatchesReference(t *testing.T) {
	inputs := marshalPlanInputs(t)
	if len(inputs) < 100 {
		t.Fatalf("only %d inputs", len(inputs))
	}
	for _, src := range inputs {
		checkMarshalPlan(t, src)
	}
}

func FuzzMarshalPlan(f *testing.F) {
	for _, src := range marshalPlanInputs(f) {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 4096 {
			return
		}
		checkMarshalPlan(t, src)
	})
}
