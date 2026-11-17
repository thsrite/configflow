// Command readers re-records native values for frozen N7-3 migration inputs.
// Reproducing the retired half requires its recorded baseline revision; no old
// YAML loader or scalar emulation remains in the working source tree.
package main

import (
	"bytes"
	"configflow/internal/logx"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type wire struct {
	Tag   string    `json:"tag,omitempty"`
	Kind  string    `json:"kind"`
	Value any       `json:"value"`
	Items []wire    `json:"items"`
	Pairs [][2]wire `json:"pairs"`
}
type record struct {
	Mode     string                `json:"mode"`
	Audit    []yamlxtest.ParseNode `json:"parse_nodes"`
	Source   string                `json:"source"`
	Native   *wire                 `json:"native"`
	Error    string                `json:"error"`
	OldError string                `json:"old_error"`
	Classes  []string              `json:"classes"`
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
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: readers collected.json native-values.json")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var input struct {
		Sources map[string]struct {
			Source string `json:"source"`
			Mode   string `json:"mode"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	output := map[string]record{}
	for id, entry := range input.Sources {
		history, err := yamlxtest.HistoricalReader(entry.Mode, entry.Source)
		if err != nil {
			return err
		}
		rec := record{Source: entry.Source, Mode: entry.Mode, OldError: history.OldError, Classes: history.Classes}
		node, nextErr := yamlx.Parse([]byte(entry.Source))
		if nextErr == nil {
			rec.Audit = yamlxtest.ParseAudit(node)
			switch entry.Mode {
			case "parser":
				nextErr = yamlx.ProjectJSONKeys(node, yamlx.RejectNonStringKeys)
			case "dialers":
				nextErr = yamlx.ProjectJSONKeys(node, yamlx.DiscardNonStringKeys)
			case "hosts":
				nextErr = subs.ProjectHostsYAMLKeys(yamlx.Get(node, "hosts"))
			case "substore":
				if proxies := yamlx.Get(node, "proxies"); proxies != nil {
					nextErr = yamlx.ProjectJSONKeys(proxies, yamlx.StringifyScalarKeys)
					if nextErr == nil {
						_, nextErr = yamlx.ToJSON(proxies)
					}
				}
			case "detection", "core-corpus", "core-review", "gateway-payload":
			default:
				return fmt.Errorf("unknown caller mode %s", entry.Mode)
			}
			if nextErr == nil {
				value, valueErr := native(node, false)
				nextErr = valueErr
				if nextErr == nil {
					rec.Native = &value
				}
			}
		}
		if nextErr != nil {
			rec.Error = nextErr.Error()
		}
		if entry.Mode == "core-corpus" || entry.Mode == "core-review" {
			rec.Native = nil
		}
		output[id] = rec
	}
	data, err = json.Marshal(output)
	if err != nil {
		return err
	}
	return os.WriteFile(os.Args[2], data, 0600)
}
func main() {
	if err := run(); err != nil {
		logx.Get("readers").Error(fmt.Sprintf("native reference failed: %v", err))
		os.Exit(1)
	}
}
