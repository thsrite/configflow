package yamlx

import (
	"errors"
	"io"
	"regexp"
	"strings"

	"configflow/internal/pyyaml"

	"gopkg.in/yaml.v3"
)

// LoadCSafeFirst is the pattern `yaml.load(text, Loader=yaml.CSafeLoader)`,
// falling back to yaml.safe_load on any exception (subscription_dialers._load,
// push_diff._load_yaml). CSafeLoader parses with libyaml but resolves and
// constructs with PyYAML's Python code, so here the text is parsed by
// gopkg.in/yaml.v3 (a libyaml port), its nodes are handed to the same
// constructor as LoadWith, and any failure (also a value the jsonx model
// cannot hold) retries with LoadWith. Documents both accept get libyaml's
// reading, as in Python.
func LoadCSafeFirst(text string, opts LoadOptions) (any, error) {
	if value, err := loadLibyaml(text, opts); err == nil {
		return value, nil
	}
	return LoadWith(text, opts)
}

func loadLibyaml(text string, opts LoadOptions) (any, error) {
	if opts.Name == "" {
		opts.Name = "<unicode string>"
	}
	source := text
retry:
	dec := yaml.NewDecoder(strings.NewReader(source))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if fixed, ok := compatible(source, err); ok {
			source = fixed
			goto retry
		}
		return nil, err
	}
	var next yaml.Node
	if err := dec.Decode(&next); err == nil || !errors.Is(err, io.EOF) {
		return nil, errors.New("not a single document")
	}
	if duplicateAnchor(&doc, map[string]bool{}) {
		return nil, errors.New("duplicate anchor")
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil, nil
		}
		root = doc.Content[0]
	}
	conv := &v3converter{name: opts.Name, resolve: resolverFor(opts.Schema), done: map[*yaml.Node]*pyyaml.Node{}}
	return constructRoot(conv.node(root), opts)
}

// duplicateAnchor is the composer's duplicate anchor check (yaml.v3 allows
// an anchor to be redefined, PyYAML's and CSafeLoader's composers do not).
func duplicateAnchor(n *yaml.Node, seen map[string]bool) bool {
	if n.Kind == yaml.AliasNode {
		return false
	}
	if n.Anchor != "" {
		if seen[n.Anchor] {
			return true
		}
		seen[n.Anchor] = true
	}
	for _, child := range n.Content {
		if duplicateAnchor(child, seen) {
			return true
		}
	}
	return false
}

// v3converter turns yaml.v3 nodes into pyyaml nodes the way CSafeLoader's
// composer does: explicit tags kept ("!!x" expanded), other scalars resolved
// with implicit = (plain or "!", not plain), aliases sharing their node.
// Marks have no snippet, like libyaml's.
type v3converter struct {
	name    string
	resolve func(string, [2]bool) string
	done    map[*yaml.Node]*pyyaml.Node
}

func longTag(tag string) string {
	if strings.HasPrefix(tag, "!!") {
		return "tag:yaml.org,2002:" + tag[2:]
	}
	return tag
}

func (v *v3converter) node(n *yaml.Node) *pyyaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if out, ok := v.done[n]; ok {
		return out
	}
	mark := pyyaml.Mark{Name: v.name, Line: n.Line - 1, Column: n.Column - 1}
	out := &pyyaml.Node{Start: mark, End: mark}
	v.done[n] = out
	explicit := n.Style&yaml.TaggedStyle != 0 && n.Tag != "" && n.Tag != "!"
	switch n.Kind {
	case yaml.SequenceNode:
		out.Kind, out.Tag, out.FlowStyle = pyyaml.SequenceNode, tagSeq, n.Style&yaml.FlowStyle != 0
		if explicit {
			out.Tag = longTag(n.Tag)
		}
		for _, child := range n.Content {
			out.Items = append(out.Items, v.node(child))
		}
	case yaml.MappingNode:
		out.Kind, out.Tag, out.FlowStyle = pyyaml.MappingNode, tagMap, n.Style&yaml.FlowStyle != 0
		if explicit {
			out.Tag = longTag(n.Tag)
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			out.Pairs = append(out.Pairs, [2]*pyyaml.Node{v.node(n.Content[i]), v.node(n.Content[i+1])})
		}
	default:
		out.Kind, out.Value = pyyaml.ScalarNode, n.Value
		quoted := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0
		switch {
		case explicit:
			out.Tag = longTag(n.Tag)
		case n.Style&yaml.TaggedStyle != 0 && n.Tag == "!":
			out.Tag = v.resolve(n.Value, [2]bool{true, false})
		default:
			out.Tag = v.resolve(n.Value, [2]bool{!quoted, quoted})
		}
	}
	return out
}

var yamlDirective = regexp.MustCompile(`(?m)^%YAML[ \t]+1\.[0-9]+`)

// compatible rewrites text for the two PyYAML behaviours yaml.v3 lacks: any
// "%YAML 1.x" directive (yaml.v3 knows 1.1 only) and the "\/" escape in
// double-quoted scalars (written as "\x2F", the same character).
func compatible(text string, err error) (string, bool) {
	message := err.Error()
	switch {
	case strings.Contains(message, "incompatible YAML document"):
		fixed := yamlDirective.ReplaceAllString(text, "%YAML 1.1")
		return fixed, fixed != text
	case strings.Contains(message, "unknown escape character"):
		fixed := escapeSlashInDoubleQuotes(text)
		return fixed, fixed != text
	}
	return text, false
}

// escapeSlashInDoubleQuotes replaces `\/` inside double-quoted scalars. A
// double-quoted scalar starts with a quote that follows the start of a line, a
// block or flow indicator (": ", "- ", "? ", ",", "[", "{") or a tag/anchor.
func escapeSlashInDoubleQuotes(text string) string {
	runes := []rune(text)
	var out strings.Builder
	inDouble, inSingle, inComment := false, false, false
	lastSignificant := rune('\n')
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inComment:
			if r == '\n' {
				inComment = false
				lastSignificant = '\n'
			}
		case inDouble:
			if r == '\\' && i+1 < len(runes) {
				if runes[i+1] == '/' {
					out.WriteString(`\x2F`)
				} else {
					out.WriteRune(r)
					out.WriteRune(runes[i+1])
				}
				i++
				continue
			}
			if r == '"' {
				inDouble = false
				lastSignificant = '"'
			}
		case inSingle:
			if r == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					out.WriteString("''")
					i++
					continue
				}
				inSingle = false
				lastSignificant = '\''
			}
		case r == '#' && (i == 0 || runes[i-1] == ' ' || runes[i-1] == '\t' || runes[i-1] == '\n'):
			inComment = true
		case r == '"' && strings.ContainsRune("\n:-?,[{!", lastSignificant):
			inDouble = true
		case r == '\'' && strings.ContainsRune("\n:-?,[{!", lastSignificant):
			inSingle = true
		case r != ' ' && r != '\t':
			lastSignificant = r
		}
		out.WriteRune(r)
	}
	return out.String()
}

