package convert

import (
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

// readProxyYAML keeps shared policy and key projection ahead of the temporary
// jsonx boundary. ProjectJSONKeys mutates only this request-owned parsed graph.
func readProxyYAML(text string) (any, error) {
	root, err := parseProxyYAML(text)
	if err != nil {
		return nil, err
	}
	if err := yamlx.ProjectJSONKeys(root, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	return yamlbridge.Value(root)
}

// readDocumentYAML preserves convert's string-key restriction and graph sharing.
// This JSON-value boundary is replaced by a typed model in N8-8.
func readDocumentYAML(text string) (any, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	if err := yamlx.ProjectJSONKeys(root, yamlx.RejectNonStringKeys); err != nil {
		return nil, err
	}
	return yamlbridge.Value(root)
}
