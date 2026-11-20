package installer

import (
	"fmt"

	"configflow/internal/yamlx"
)

// readCompose adapts the generated Compose document to a JSON map for test inspection.
func readCompose(text string) (map[string]any, error) {
	document, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, fmt.Errorf(msgComposeInvalid, err)
	}
	data, err := yamlx.ToJSON(document)
	if err != nil {
		return nil, fmt.Errorf(msgComposeInvalid, err)
	}
	value, err := parseJSONAny(data)
	if err != nil {
		return nil, fmt.Errorf(msgComposeInvalid, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(msgComposeMapping)
	}
	return object, nil
}
