package mosdns

import (
	"configflow/internal/yamlx"
	"errors"
	"gopkg.in/yaml.v3"
)

// UnsupportedYAMLError reports a custom MosDNS base configuration
// (mosdns.custom_config) or an upstream definition with a loaded YAML value
// the jsonx model cannot hold that Python would copy into the generated
// configuration: a collection that contains itself through an alias, or a
// !!set/!!omap/!!pairs value. Python writes such values with yaml.Dumper;
// the Go server refuses instead of writing other bytes. Values only in
// sections Python drops do not matter; mappings with keys that are not
// strings are written like PyYAML.
type UnsupportedYAMLError struct {
	Msg   string
	cause error
}

func (e *UnsupportedYAMLError) Error() string { return e.Msg }

// Unwrap retains the facade policy or alias rejection.
func (e *UnsupportedYAMLError) Unwrap() error { return e.cause }

// loadCustomConfig keeps free-form configuration as a graph. Ordinary syntax
// errors retain the established default-template fallback; policy and unsafe
// alias errors are never hidden by that fallback.
func loadCustomConfig(text string) (*yamlx.Node, error) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		if !errors.Is(err, yamlx.ErrSyntax) {
			return nil, customYAMLError(err)
		}
		return nil, nil
	}
	if node.Kind != yaml.MappingNode || node.ShortTag() == "!!set" {
		return nil, nil
	}
	return node, nil
}
