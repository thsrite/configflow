package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// The frozen ruamel corpora remain input coverage; byte-format expectations are
// retired. The oracle applies the declared relink operation to an independently
// expanded native tree, then checks the entire resulting document and comments.
func TestCommentedCorpus(t *testing.T) { runCommentedCorpus(t, "commented.jsonl.gz", 2411) }
func TestCommentedAnchorsCorpus(t *testing.T) {
	runCommentedCorpus(t, "commented_anchors.jsonl.gz", 1965)
}
func TestCommentedRealisticCorpus(t *testing.T) {
	runCommentedCorpus(t, "commented_realistic.jsonl.gz", 4911)
}
func TestCommentedScalarsCorpus(t *testing.T) {
	runCommentedCorpus(t, "commented_scalars.jsonl.gz", 2080)
}
func TestCommentedBlocksCorpus(t *testing.T) {
	runCommentedCorpus(t, "commented_blocks.jsonl.gz", 1112)
}

func runCommentedCorpus(t *testing.T, file string, count int) {
	t.Helper()
	rows := loadCorpus(t, file)
	if len(rows) != count {
		t.Fatalf("truncated frozen corpus: %d want %d", len(rows), count)
	}
	stats := map[string]int{}
	for i, row := range rows {
		var in struct {
			Config string  `json:"config"`
			Old    string  `json:"old"`
			New    *string `json:"new"`
		}
		if err := json.Unmarshal(row.InRaw, &in); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
		source := in.Config
		profile := &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: source}}
		newName := in.New
		if strings.TrimSpace(source) == "" || newName != nil && *newName == in.Old {
			changed, err := RelinkListeners(profile, in.Old, newName)
			if err != nil || len(changed) != 0 || profile.Mihomo.CustomConfig != source {
				t.Errorf("row %d no-op changed", i)
			}
			stats["no-op"]++
			continue
		}
		original, parseErr := yamlx.Parse([]byte(source))
		var expected *yamlx.Node
		var paths []string
		var expectedErr error
		if parseErr == nil {
			expected, paths, expectedErr = listenerCorpusExpected(original, in.Old, newName)
		}
		changed, err := RelinkListeners(profile, in.Old, newName)
		result := profile.Mihomo.CustomConfig
		if parseErr != nil || expectedErr != nil {
			if err == nil || result != source {
				t.Errorf("row %d invalid input changed or accepted: %v", i, err)
				continue
			}
			if errors.Is(parseErr, yamlx.ErrDuplicateKey) {
				var duplicate *yamlx.DuplicateKeyError
				if !errors.Is(err, yamlx.ErrDuplicateKey) || !errors.As(err, &duplicate) || duplicate.Line < 1 || duplicate.Key == "" {
					t.Errorf("row %d lost duplicate diagnostic: %v", i, err)
				}
				stats["duplicate-rejected"]++
			} else if parseErr != nil {
				if !strings.HasPrefix(err.Error(), msgListenerBaseParse) {
					t.Errorf("row %d wrong parse context: %v", i, err)
				}
				stats["native-parse-rejected"]++
			} else {
				if err.Error() != expectedErr.Error() {
					t.Errorf("row %d validation %v want %v", i, err, expectedErr)
				}
				stats["schema-rejected"]++
			}
			continue
		}
		if err != nil {
			stats["editor-refused"]++
			if stats["editor-refused"] <= 8 {
				t.Errorf("row %d valid edit refused: %v (%s)", i, err, causeOf(err))
			}
			continue
		}
		actual, err := yamlx.Parse([]byte(result))
		if err != nil || !yamlx.Equal(actual, expected) || strings.Join(changed, ",") != strings.Join(paths, ",") {
			stats["mismatch"]++
			if stats["mismatch"] <= 6 {
				t.Errorf("row %d wrong semantics/paths: %v got paths %v want %v", i, err, changed, paths)
			}
			continue
		}
		if len(paths) == 0 && result != source {
			t.Errorf("row %d no-op changed bytes", i)
		}
		have := listenerCommentCounts(actual)
		for text, n := range listenerCommentCounts(original) {
			if have[text] < n && strings.Count(result, text) < n {
				t.Errorf("row %d lost comment %q", i, text)
			}
		}
		if strings.Contains(source, "\r\n") && !strings.ContainsAny(strings.ReplaceAll(source, "\r\n", ""), "\r\n") && strings.Contains(strings.ReplaceAll(result, "\r\n", ""), "\n") {
			t.Errorf("row %d CRLF changed to bare LF", i)
		}
		if _, _, isErr := isErrorRaw(row.OutRaw); isErr {
			stats["python-emitter-or-schema-error-recovered"]++
		}
		stats["semantic-and-comments-match"]++
	}
	t.Logf("%s: %d rows %v", file, len(rows), stats)
	if stats["editor-refused"] != 0 {
		t.Errorf("editor refusals: %d", stats["editor-refused"])
	}
}

func listenerCorpusExpected(original *yamlx.Node, oldName string, newName *string) (*yamlx.Node, []string, error) {
	doc := yamlx.Clone(original)
	if doc.Kind != yaml.MappingNode || doc.ShortTag() != "!!map" {
		return nil, nil, lre(msgListenerBaseObject)
	}
	listeners := yamlx.Get(doc, "listeners")
	if listeners == nil || listeners.Kind == yaml.ScalarNode && listeners.ShortTag() == "!!null" {
		return doc, nil, nil
	}
	if listeners.Kind != yaml.SequenceNode || listeners.ShortTag() != "!!seq" {
		return nil, nil, lre(msgListenerListRequired)
	}
	var paths []string
	for i, item := range listeners.Content {
		if item.Kind != yaml.MappingNode || item.ShortTag() != "!!map" {
			return nil, nil, lre(fmt.Sprintf(msgListenerObjectRequired, i))
		}
		proxy := yamlx.Get(item, "proxy")
		if proxy == nil || proxy.Kind == yaml.ScalarNode && proxy.ShortTag() == "!!null" {
			continue
		}
		if proxy.Kind != yaml.ScalarNode || proxy.ShortTag() != "!!str" {
			return nil, nil, lre(fmt.Sprintf(msgListenerTargetRequired, i))
		}
		if strings.Contains(proxy.Value, "${") {
			return nil, nil, lre(fmt.Sprintf(msgListenerVariable, i))
		}
		if proxy.Value != oldName {
			continue
		}
		paths = append(paths, fmt.Sprintf("listeners[%d].proxy", i))
		if newName == nil {
			yamlx.Delete(item, "proxy")
		} else {
			yamlx.Set(item, "proxy", yamlx.Str(*newName))
		}
	}
	return doc, paths, nil
}

func listenerCommentCounts(doc *yamlx.Node) map[string]int {
	out := map[string]int{}
	seen := map[*yamlx.Node]bool{}
	var walk func(*yamlx.Node)
	walk = func(n *yamlx.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		for _, text := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			for _, line := range strings.Split(text, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					out[line]++
				}
			}
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(doc)
	return out
}

func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b || i >= len(g) || i >= len(w) {
			return fmt.Sprintf("line %d (of got %d, want %d)\n got:  %q\n want: %q", i+1, len(g), len(w), a, b)
		}
	}
	return "equal"
}

func causeOf(err error) string {
	var le *ListenerReferenceError
	if errors.As(err, &le) && le.Cause != nil {
		s := le.Cause.Error()
		s = strings.TrimPrefix(s, "comment-preserving edit not supported: ")
		if i := strings.Index(s, " at line"); i > 0 {
			s = s[:i]
		}
		return s
	}
	return err.Error()
}
