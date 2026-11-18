package convert

import (
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// subscriptionSources wires the typed parser; it owns no protocol or filtering
// business and never turns a whole config into JSON for an individual node.
func subscriptionSources(text string) ([]ClientSource, error) {
	parsed := subs.ParseList(text)
	result := make([]ClientSource, 0, len(parsed))
	for _, item := range parsed {
		params := item.Raw
		if params != nil {
			// The old API's transport marker is not an engine option.
			yamlx.Delete(params, "_raw_object")
		} else {
			var err error
			params, err = item.Node.Params.Node()
			if err != nil {
				return nil, err
			}
		}
		result = append(result, ClientSource{Node: item.Node, Params: params})
	}
	return result, nil
}
