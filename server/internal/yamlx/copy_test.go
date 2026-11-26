package yamlx

import (
	"fmt"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func amplifiedCopyGraph(depth int) *Node {
	root := Str("leaf")
	root.Anchor = "leaf"
	for i := 0; i < depth; i++ {
		root = Seq(&Node{Kind: yaml.AliasNode, Value: root.Anchor, Alias: root}, &Node{Kind: yaml.AliasNode, Value: root.Anchor, Alias: root})
		root.Anchor = fmt.Sprintf("level%d", i)
	}
	return root
}
func assertGraphCopy(t testing.TB, source, copied *Node) int {
	t.Helper()
	seen := map[*Node]*Node{}
	reverse := map[*Node]*Node{}
	var visit func(*Node, *Node)
	visit = func(a, b *Node) {
		if a == nil || b == nil {
			if a != b {
				t.Fatal("nil edge changed")
			}
			return
		}
		if previous, ok := seen[a]; ok {
			if b != previous {
				t.Fatal("shared edge duplicated")
			}
			return
		}
		if a == b {
			t.Fatal("copy retains source node")
		}
		if other, ok := reverse[b]; ok && other != a {
			t.Fatal("unrelated nodes coalesced")
		}
		seen[a] = b
		reverse[b] = a
		if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || a.Style != b.Style || a.Anchor != b.Anchor || a.HeadComment != b.HeadComment || a.LineComment != b.LineComment || a.FootComment != b.FootComment || len(a.Content) != len(b.Content) {
			t.Fatal("node metadata changed")
		}
		visit(a.Alias, b.Alias)
		for i := range a.Content {
			visit(a.Content[i], b.Content[i])
		}
	}
	visit(source, copied)
	return len(seen)
}
func TestCopyGraphOwnershipAndSharing(t *testing.T) {
	shared := Map(Str("value"), Str("unchanged"))
	shared.Anchor = "shared"
	shared.HeadComment = "owned"
	source := Seq(shared, &Node{Kind: yaml.AliasNode, Value: "shared", Alias: shared}, shared)
	copy := CopyGraph(source)
	assertGraphCopy(t, source, copy)
	Set(copy.Content[0], "value", Str("changed"))
	copy.Content[0].HeadComment = "changed"
	if Get(shared, "value").Value != "unchanged" || shared.HeadComment != "owned" {
		t.Fatal("copy modified input")
	}
	if copy.Content[1].Alias != copy.Content[0] || copy.Content[2] != copy.Content[0] {
		t.Fatal("sharing changed")
	}
}
func TestCopyGraphAliasAmplificationStaysPhysical(t *testing.T) {
	for _, depth := range []int{8, 64, 256} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			source := amplifiedCopyGraph(depth)
			count := assertGraphCopy(t, source, CopyGraph(source))
			if count != 3*depth+1 {
				t.Fatalf("physical nodes=%d", count)
			}
			allocations := testing.AllocsPerRun(10, func() { copyGraphSink = CopyGraph(source) })
			if allocations > float64(count*3+20) {
				t.Fatalf("alias expansion allocated %.0f objects for %d physical nodes", allocations, count)
			}
		})
	}
	// Copying does not launder invalid source graphs past the facade budget.
	source := amplifiedCopyGraph(64)
	if _, err := Marshal(CopyGraph(source)); err == nil {
		t.Fatal("over-budget graph escaped writer")
	}
	cycle := Seq()
	cycle.Content = append(cycle.Content, &Node{Kind: yaml.AliasNode, Alias: cycle})
	copied := CopyGraph(cycle)
	assertGraphCopy(t, cycle, copied)
	if copied.Content[0].Alias != copied {
		t.Fatal("cycle truncated")
	}
}
func TestCopyGraphConcurrentEdits(t *testing.T) {
	source := Map(Str("nested"), Map(Str("value"), Str("original")))
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 100; n++ {
				copy := CopyGraph(source)
				Set(Get(copy, "nested"), "value", Str("edited"))
			}
		}()
	}
	workers.Wait()
	if Get(Get(source, "nested"), "value").Value != "original" {
		t.Fatal("concurrent copy mutated source")
	}
}

var copyGraphSink *Node

func BenchmarkCopyGraphPhysicalMemory(b *testing.B) {
	for _, depth := range []int{8, 64, 256} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			source := amplifiedCopyGraph(depth)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				copyGraphSink = CopyGraph(source)
			}
		})
	}
}
func FuzzCopyGraph(f *testing.F) {
	f.Add([]byte{1, 2, 3, 0, 1})
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			data = data[:512]
		}
		nodes := make([]*Node, len(data)+1)
		for i := range nodes {
			nodes[i] = Seq()
		}
		for i, value := range data {
			if value%2 == 0 {
				nodes[i].Content = append(nodes[i].Content, nodes[int(value)%len(nodes)])
			} else {
				nodes[i].Alias = nodes[int(value)%len(nodes)]
				nodes[i].Kind = yaml.AliasNode
			}
		}
		assertGraphCopy(t, nodes[0], CopyGraph(nodes[0]))
	})
}
