// Command yamlwriteroracle selects independently recorded Python safe_load
// graphs whose old YAML text has a different interpretation in yaml.v3.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"configflow/internal/logx"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"

	"gopkg.in/yaml.v3"
)

func main() {
	input := flag.String("in", "", "Python JSONL graph records")
	output := flag.String("out", "", "selected gzip JSON references")
	stream := flag.Bool("stream", false, "render/compare recorded graphs as a JSONL stream")
	flag.Parse()
	var err error
	if *stream {
		err = writerStream(os.Stdin, os.Stdout)
	} else {
		err = run(*input, *output)
	}
	if err != nil {
		logx.Get("yamlwriteroracle").Error(fmt.Sprintf("writer oracle generation failed: %v", err))
		os.Exit(1)
	}
}

func writerStream(input io.Reader, output io.Writer) error {
	d, e := json.NewDecoder(input), json.NewEncoder(output)
	e.SetEscapeHTML(false)
	for {
		var request struct {
			yamlxtest.WriterReference
			Op  string `json:"op"`
			Old string `json:"old"`
			New string `json:"new"`
		}
		if err := d.Decode(&request); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		answer := map[string]any{}
		if request.Op == "compare" {
			a, err := yamlx.Parse([]byte(request.Old))
			if err != nil {
				answer["error"] = err.Error()
			} else if b, err := yamlx.Parse([]byte(request.New)); err != nil {
				answer["error"] = err.Error()
			} else {
				var changes []yamlxtest.WriterChange
				if err := writerChanges(a, b, "", &changes, map[[2]*yamlx.Node]bool{}); err != nil {
					answer["error"] = err.Error()
				} else {
					answer["changes"] = changes
				}
			}
		} else {
			n, err := request.Document()
			if err == nil {
				n, err = yamlbridge.Encode(n)
			}
			if err == nil {
				var data []byte
				data, err = yamlx.Marshal(n)
				answer["yaml"] = string(data)
			}
			if err != nil {
				answer["error"] = err.Error()
			}
		}
		if err := e.Encode(answer); err != nil {
			return err
		}
	}
}

func writerChanges(old, want *yamlx.Node, path string, out *[]yamlxtest.WriterChange, seen map[[2]*yamlx.Node]bool) error {
	old, want = yamlx.Resolve(old), yamlx.Resolve(want)
	if yamlx.Equal(old, want) || seen[[2]*yamlx.Node{old, want}] {
		return nil
	}
	seen[[2]*yamlx.Node{old, want}] = true
	if old.Kind != want.Kind {
		return fmt.Errorf("unexpected node shape change at %s", path)
	}
	if want.Kind == yaml.MappingNode {
		a, b := map[string]*yamlx.Node{}, map[string]*yamlx.Node{}
		for key, value := range yamlx.Pairs(old) {
			a[key] = value
		}
		for key, value := range yamlx.Pairs(want) {
			b[key] = value
		}
		if len(a) != len(b) {
			return fmt.Errorf("unexpected mapping field change at %s", path)
		}
		for key, value := range yamlx.Pairs(want) {
			before, ok := a[key]
			if !ok {
				return fmt.Errorf("unexpected mapping key change at %s", path)
			}
			if err := writerChanges(before, value, path+"."+key, out, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if want.Kind == yaml.SequenceNode {
		if len(old.Content) != len(want.Content) {
			return fmt.Errorf("unexpected sequence change at %s", path)
		}
		for i, child := range want.Content {
			if err := writerChanges(old.Content[i], child, fmt.Sprintf("%s[%d]", path, i), out, seen); err != nil {
				return err
			}
		}
		return nil
	}
	category := ""
	restored := map[string]int{}
	for _, ch := range []rune{'\u0085', '\u2028', '\u2029'} {
		if count := strings.Count(want.Value, string(ch)) - strings.Count(old.Value, string(ch)); count > 0 {
			restored[fmt.Sprintf("U+%04X", ch)] = count
		}
	}
	switch {
	case want.ShortTag() == "!!str" && (old.ShortTag() == "!!int" || old.ShortTag() == "!!float" || old.ShortTag() == "!!bool" || old.ShortTag() == "!!null"):
		category = "string-type-preservation"
	case want.ShortTag() == "!!int" && old.ShortTag() == "!!float":
		category = "large-int-preservation"
	case want.ShortTag() == "!!timestamp" && old.ShortTag() == "!!str":
		category = "timestamp-preservation"
	case want.ShortTag() == "!!str" && old.ShortTag() == "!!str" && len(restored) > 0:
		category = "unicode-line-separator-preservation"
	default:
		a, b := []rune(old.Value), []rune(want.Value)
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		var before, after rune
		if i < len(a) {
			before = a[i]
		}
		if i < len(b) {
			after = b[i]
		}
		return fmt.Errorf("unclassified scalar change at %s (%s -> %s; rune %d U+%04X -> U+%04X; lengths %d/%d)", path, old.ShortTag(), want.ShortTag(), i, before, after, len(a), len(b))
	}
	if category != "unicode-line-separator-preservation" {
		restored = nil
	}
	*out = append(*out, yamlxtest.WriterChange{Path: path, Before: old.ShortTag(), After: want.ShortTag(), Category: category, Restored: restored})
	return nil
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
