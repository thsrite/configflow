// Command yamlwriteroracle selects independently recorded Python safe_load
// graphs whose old YAML text has a different interpretation in yaml.v3.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"configflow/internal/logx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"

	"gopkg.in/yaml.v3"
)

func main() {
	input := flag.String("in", "", "Python JSONL graph records")
	output := flag.String("out", "", "selected gzip JSON references")
	flag.Parse()
	if err := run(*input, *output); err != nil {
		logx.Get("yamlwriteroracle").Error(fmt.Sprintf("writer oracle generation failed: %v", err))
		os.Exit(1)
	}
}

func run(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 64*1024*1024)
	refs := []yamlxtest.WriterReference{}
	counts := map[string]int{}
	total := 0
	for s.Scan() {
		var r yamlxtest.WriterReference
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return err
		}
		total++
		want, err := r.Document()
		if err != nil {
			return err
		}
		old, err := yamlx.Parse([]byte(r.Text))
		if err != nil {
			return fmt.Errorf("parse old output %s:%d: %w", r.Corpus, r.Row, err)
		}
		if yamlx.Equal(old, want) {
			continue
		}
		if err := changes(old, want, "", &r.Changes, map[[2]*yamlx.Node]bool{}); err != nil {
			return fmt.Errorf("classify %s:%d: %w", r.Corpus, r.Row, err)
		}
		if len(r.Changes) == 0 {
			return fmt.Errorf("unclassified output %s:%d", r.Corpus, r.Row)
		}
		rowCategories := map[string]bool{}
		for _, c := range r.Changes {
			rowCategories[c.Category] = true
		}
		for category := range rowCategories {
			counts[category]++
		}
		r.Text = "" // Digest + corpus/row preserve the unchanged source.
		refs = append(refs, r)
	}
	if err := s.Err(); err != nil {
		return err
	}
	fout, err := os.Create(output)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(fout)
	if err := json.NewEncoder(gz).Encode(refs); err != nil {
		gz.Close()
		fout.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		fout.Close()
		return err
	}
	if err := fout.Close(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"total": total, "changed": len(refs), "categories": counts, "unknown": 0})
}

func changes(old, want *yamlx.Node, path string, out *[]yamlxtest.WriterChange, seen map[[2]*yamlx.Node]bool) error {
	old, want = yamlx.Resolve(old), yamlx.Resolve(want)
	if yamlx.Equal(old, want) || seen[[2]*yamlx.Node{old, want}] {
		return nil
	}
	seen[[2]*yamlx.Node{old, want}] = true
	if old.Kind != want.Kind || len(old.Content) != len(want.Content) {
		return fmt.Errorf("unexpected container change at %s", path)
	}
	if old.Kind == yaml.ScalarNode {
		category := ""
		switch {
		case want.ShortTag() == "!!str" && (old.ShortTag() == "!!int" || old.ShortTag() == "!!float"):
			category = "string-read-as-number"
		case want.ShortTag() == "!!int" && old.ShortTag() == "!!float":
			category = "large-int-read-as-float"
		case want.ShortTag() == "!!timestamp" && old.ShortTag() == "!!str":
			category = "timestamp-read-as-string"
		default:
			return fmt.Errorf("unexpected scalar change at %s (%s -> %s)", path, old.ShortTag(), want.ShortTag())
		}
		*out = append(*out, yamlxtest.WriterChange{Path: path, Before: old.ShortTag(), After: want.ShortTag(), Category: category})
		return nil
	}
	for i := range old.Content {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		if old.Kind == yaml.MappingNode {
			key := yamlx.Resolve(want.Content[i-i%2])
			childPath = path + "." + key.Value
			if i%2 == 0 {
				childPath += "(key)"
			}
		}
		if err := changes(old.Content[i], want.Content[i], childPath, out, seen); err != nil {
			return err
		}
	}
	return nil
}
