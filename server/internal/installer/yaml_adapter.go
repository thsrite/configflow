package installer

import (
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// readCompose adapts the generated Compose document to the existing bundle
// metadata interface. Both parsing and JSON materialization enforce the facade's
// alias budget; no user graph is cloned or recursively expanded here.
func readCompose(text string) (*jsonx.Object, error) {
	document, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(document)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("decode Compose: document must be a mapping")
	}
	return object, nil
}
