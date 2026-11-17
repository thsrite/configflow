// Command readers records native YAML values and classifies old/new loads for
// the N7-3 reader migration. It never calls a Go business function.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pyyaml"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type wire struct {
	Tag   string    `json:"tag,omitempty"`
	Kind  string    `json:"kind"`
	Value any       `json:"value"`
	Items []wire    `json:"items"`
	Pairs [][2]wire `json:"pairs"`
}
type record struct {
	Mode     string      `json:"mode"`
	Audit    []auditNode `json:"parse_nodes"`
	Source   string      `json:"source"`
	Native   *wire       `json:"native"`
	Error    string      `json:"error"`
	OldError string      `json:"old_error"`
	Classes  []string    `json:"classes"`
}

func native(n *yamlx.Node, isKey bool) (wire, error) {
	n = yamlx.Resolve(n)
	if isKey && n.Kind == yaml.ScalarNode {
		switch n.ShortTag() {
		case "!!str", "!!bool", "!!int", "!!float", "!!null":
		default:
			return wire{Kind: "tagged-key", Tag: n.Tag, Value: n.Value}, nil
		}
	}
	switch n.Kind {
	case yaml.MappingNode:
		w := wire{Kind: "mapping", Pairs: [][2]wire{}}
		for key, value := range yamlx.AllPairs(n) {
			k, err := native(key, true)
			if err != nil {
				return w, err
			}
			v, err := native(value, false)
			if err != nil {
				return w, err
			}
			w.Pairs = append(w.Pairs, [2]wire{k, v})
		}
		return w, nil
	case yaml.SequenceNode:
		w := wire{Kind: "sequence", Items: []wire{}}
		for _, child := range n.Content {
			v, err := native(child, false)
			if err != nil {
				return w, err
			}
			w.Items = append(w.Items, v)
		}
		return w, nil
	default:
		w := wire{Kind: "scalar"}
		switch n.ShortTag() {
		case "!!null":
			w.Value = nil
		case "!!bool", "!!int", "!!float":
			// Wire encoding of a parsed scalar follows the facade's JSON scalar policy
			// (not a consumer result), including arbitrary integers and non-finite text.
			data, err := yamlx.ToJSON(n)
			if err != nil {
				return w, err
			}
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			if err := d.Decode(&w.Value); err != nil {
				return w, err
			}
		default:
			w.Value = n.Value
		}
		return w, nil
	}
}
func legacy(v any) wire {
	switch x := v.(type) {
	case *jsonx.Object:
		w := wire{Kind: "mapping", Pairs: [][2]wire{}}
		for key, value := range x.All() {
			w.Pairs = append(w.Pairs, [2]wire{legacy(key), legacy(value)})
		}
		return w
	case *yamlx.Mapping:
		w := wire{Kind: "mapping", Pairs: [][2]wire{}}
		for _, p := range x.Pairs {
			w.Pairs = append(w.Pairs, [2]wire{legacy(p.Key), legacy(p.Value)})
		}
		return w
	case []any:
		w := wire{Kind: "sequence", Items: []wire{}}
		for _, v := range x {
			w.Items = append(w.Items, legacy(v))
		}
		return w
	case yamlx.Timestamp:
		return wire{Kind: "scalar", Value: map[string]string{"__date__": x.String()}}
	case []byte:
		return wire{Kind: "scalar", Value: map[string]string{"__bytes__": hex.EncodeToString(x)}}
	case jsonx.Number:
		data, err := x.MarshalJSON()
		if err != nil {
			return wire{Kind: "scalar", Value: map[string]string{"__nonfinite__": x.String()}}
		}
		return wire{Kind: "scalar", Value: json.Number(string(data))}
	default:
		return wire{Kind: "scalar", Value: v}
	}
}
func canonical(w wire) string {
	if w.Kind == "scalar" {
		if num, ok := w.Value.(json.Number); ok {
			if r, ok := new(big.Rat).SetString(num.String()); ok {
				return "number:" + r.RatString()
			}
		}
		b, _ := json.Marshal(w.Value)
		return "scalar:" + string(b)
	}
	var parts []string
	if w.Kind == "sequence" {
		for _, v := range w.Items {
			parts = append(parts, canonical(v))
		}
	} else {
		for _, p := range w.Pairs {
			parts = append(parts, canonical(p[0])+"="+canonical(p[1]))
		}
		sort.Strings(parts)
	}
	return w.Kind + "[" + strings.Join(parts, ",") + "]"
}
func hints(n *yamlx.Node, old *pyyaml.Node) map[string]bool {
	classes := map[string]bool{}
	oldTags := map[string]string{}
	oldSeen := map[*pyyaml.Node]bool{}
	var scanOld func(*pyyaml.Node)
	scanOld = func(n *pyyaml.Node) {
		if n == nil || oldSeen[n] {
			return
		}
		oldSeen[n] = true
		oldTags[fmt.Sprintf("%d:%d:%s", n.Start.Line+1, n.Start.Column+1, n.Value)] = strings.TrimPrefix(n.Tag, "tag:yaml.org,2002:")
		for _, c := range n.Items {
			scanOld(c)
		}
		for _, p := range n.Pairs {
			scanOld(p[0])
			scanOld(p[1])
		}
	}
	scanOld(old)
	type role struct {
		n   *yamlx.Node
		key bool
	}
	seen := map[role]bool{}
	var scan func(*yamlx.Node, bool)
	scan = func(n *yamlx.Node, key bool) {
		if n == nil || seen[role{n, key}] {
			return
		}
		seen[role{n, key}] = true
		tag := n.ShortTag()
		if n.Kind == yaml.ScalarNode {
			oldTag, ok := oldTags[fmt.Sprintf("%d:%d:%s", n.Line, n.Column, n.Value)]
			if ok && "!!"+oldTag != tag && n.Style&yaml.TaggedStyle == 0 {
				classes["implicit-schema"] = true
			}
			switch tag {
			case "!!timestamp":
				classes["timestamp-text"] = true
			case "!!binary":
				classes["binary-text"] = true
			case "!!float":
				if strings.Contains(strings.ToLower(n.Value), "nan") || strings.Contains(strings.ToLower(n.Value), "inf") {
					classes["nonfinite-text"] = true
				}
			}
		}
		switch tag {
		case "!!null", "!!bool", "!!int", "!!float", "!!str", "!!timestamp", "!!binary", "!!seq", "!!map":
		case "!!merge":
			if !key {
				classes["non-json-tags"] = true
			}
		default:
			classes["non-json-tags"] = true
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := yamlx.Resolve(n.Content[i])
				if k.Kind != yaml.ScalarNode || k.ShortTag() != "!!str" && k.ShortTag() != "!!merge" {
					classes["non-string-keys"] = true
				}
			}
		}
		for i, c := range n.Content {
			scan(c, n.Kind == yaml.MappingNode && i%2 == 0)
		}
		scan(n.Alias, key)
	}
	scan(n, false)
	return classes
}

var surrogateEscape = regexp.MustCompile(`\\u[dD][89aAbB][[:xdigit:]]{2}`)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: readers collected.json native-values.json")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var input struct {
		Sources map[string]struct {
			Source string `json:"source"`
			Mode   string `json:"mode"`
		} `json:"sources"`
	}
	if err = json.Unmarshal(data, &input); err != nil {
		panic(err)
	}
	output := map[string]record{}
	counts := map[string]int{}
	unknown := 0
	for id, entry := range input.Sources {
		source := entry.Source
		rec := record{Source: source, Mode: entry.Mode}
		var oldValue any
		var oldErr error
		switch entry.Mode {
		case "parser", "core-corpus":
			oldValue, oldErr = yamlx.SafeLoad(source)
		case "dialers":
			oldValue, oldErr = yamlx.LoadCSafeFirst(source, yamlx.LoadOptions{KeepNonStringKeys: true})
		default:
			oldValue, oldErr = yamlx.LoadWith(source, yamlx.LoadOptions{KeepNonStringKeys: true})
		}
		if oldErr != nil {
			rec.OldError = oldErr.Error()
		}
		node, newErr := yamlx.Parse([]byte(source))
		classes := map[string]bool{}
		if newErr == nil {
			rec.Audit = audit(node)
			switch entry.Mode {
			case "parser", "dialers":
				mode := yamlx.RejectNonStringKeys
				if entry.Mode == "dialers" {
					mode = yamlx.DiscardNonStringKeys
				}
				newErr = yamlx.ProjectJSONKeys(node, mode)
			case "hosts":
				newErr = subs.ProjectHostsYAMLKeys(yamlx.Get(node, "hosts"))
			case "substore":
				if proxies := yamlx.Get(node, "proxies"); proxies != nil {
					newErr = yamlx.ProjectJSONKeys(proxies, yamlx.StringifyScalarKeys)
					if newErr == nil {
						_, newErr = yamlx.ToJSON(proxies)
					}
				}
			case "detection", "core-corpus", "core-review":
			default:
				panic("unknown caller mode")
			}
			if newErr == nil {
				w, err := native(node, false)
				newErr = err
				if err == nil {
					rec.Native = &w
				}
			}
		}
		if newErr != nil {
			rec.Error = newErr.Error()
			switch {
			case errors.Is(newErr, yamlx.ErrDuplicateKey):
				classes["D1-duplicate"] = true
			case errors.Is(newErr, yamlx.ErrAliasRecursion):
				classes["alias-recursion"] = true
			case errors.Is(newErr, yamlx.ErrAliasBudget):
				classes["alias-budget"] = true
			case errors.Is(newErr, yamlx.ErrType) || strings.Contains(newErr.Error(), "JSON object key"):
				classes["native-json-key-policy"] = true
			case errors.Is(newErr, yamlx.ErrPolicy):
				switch {
				case strings.Contains(newErr.Error(), "标量内容无效"):
					classes["explicit-scalar-validation"] = true
				case strings.Contains(newErr.Error(), "必须用于"):
					classes["tag-shape"] = true
				case strings.Contains(newErr.Error(), "合并引用"):
					classes["merge-source"] = true
				case strings.Contains(newErr.Error(), "只能包含一个文档"):
					classes["multiple-documents"] = true
				case strings.Contains(newErr.Error(), "缺少类型名"):
					classes["empty-tag"] = true
				default:
					classes["policy-validation"] = true
				}
			case errors.Is(newErr, yamlx.ErrSyntax) && oldErr == nil && surrogateEscape.MatchString(source):
				classes["native-parser-surrogate"] = true
			case errors.Is(newErr, yamlx.ErrSyntax) && oldErr == nil && (strings.HasPrefix(source, "%YAML 1.2\n") || strings.HasPrefix(source, "\ufeff\ufeff") || strings.Contains(source, `\/`)):
				classes["native-parser-A"] = true
			case oldErr != nil:
				classes["native-diagnostics"] = true
			default:
				classes["UNCLASSIFIED"] = true
			}
		} else if oldErr == nil && canonical(legacy(oldValue)) == canonical(*rec.Native) {
			oldData, _ := json.Marshal(legacy(oldValue))
			newData, _ := json.Marshal(rec.Native)
			if bytes.Equal(oldData, newData) {
				classes["equal"] = true
			} else {
				classes["native-format"] = true
			}
		} else {
			oldNode, _ := pyyaml.Compose(source)
			rawNode, _ := yamlx.Parse([]byte(source))
			classes = hints(rawNode, oldNode)
			if strings.HasPrefix(source, "\ufeff\ufeff") && oldErr == nil {
				classes["native-parser-C"] = true
			}
			if oldErr != nil && (strings.Contains(oldErr.Error(), "duplicate anchor") || strings.Contains(oldErr.Error(), "maximum recursion depth")) {
				classes["native-parser-B"] = true
			}
			if oldErr != nil && (strings.Contains(source, `!!int "0X1F"`) || strings.Contains(source, `!!int "09"`) || strings.Contains(oldErr.Error(), "4300 digits")) {
				classes["explicit-scalar-native"] = true
			}
			if len(classes) == 0 {
				if oldErr != nil && strings.Contains(oldErr.Error(), "\\t") && strings.ContainsRune(source, '\t') {
					classes["native-parser-tab"] = true
				} else {
					classes["UNCLASSIFIED"] = true
				}
			}
		}
		for class := range classes {
			rec.Classes = append(rec.Classes, class)
			counts[class]++
			if class == "UNCLASSIFIED" {
				unknown++
				fmt.Fprintf(os.Stderr, "unclassified loader %s: %q\n", id, source)
			}
		}
		sort.Strings(rec.Classes)
		if strings.HasPrefix(entry.Mode, "core-") {
			rec.Native = nil
		}
		output[id] = rec
	}
	out, err := json.Marshal(output)
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(os.Args[2], out, 0600); err != nil {
		panic(err)
	}
	summary, _ := json.Marshal(counts)
	fmt.Println(string(summary))
	if unknown != 0 {
		os.Exit(1)
	}
}

type auditNode struct {
	ID           int       `json:"id"`
	Kind         yaml.Kind `json:"kind"`
	Tag          string    `json:"tag"`
	Value        string    `json:"value"`
	Line, Column int
	Content      []int `json:"content"`
	Alias        int   `json:"alias"`
}

func audit(root *yamlx.Node) []auditNode {
	ids := map[*yamlx.Node]int{}
	var result []auditNode
	var visit func(*yamlx.Node) int
	visit = func(n *yamlx.Node) int {
		if n == nil {
			return 0
		}
		if id := ids[n]; id != 0 {
			return id
		}
		id := len(result) + 1
		ids[n] = id
		result = append(result, auditNode{ID: id, Kind: n.Kind, Tag: n.Tag, Value: n.Value, Line: n.Line, Column: n.Column})
		var children []int
		for _, child := range n.Content {
			children = append(children, visit(child))
		}
		alias := visit(n.Alias)
		result[id-1].Content = children
		result[id-1].Alias = alias
		return id
	}
	visit(root)
	return result
}
