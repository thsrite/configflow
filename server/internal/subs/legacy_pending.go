package subs

// Temporary boundaries still called by the pending chain/dialer owners.
import "configflow/internal/jsonx"

func listField(o *jsonx.Object, key string) []any {
	l, _ := o.Value(key).([]any)
	return l
}

func objectsByID(items []any, enabledOnly bool) (map[string]*jsonx.Object, []string) {
	m := map[string]*jsonx.Object{}
	var order []string
	for _, item := range items {
		o, ok := item.(*jsonx.Object)
		if !ok {
			continue
		}
		id, ok := o.Value("id").(string)
		if !ok || id == "" {
			continue
		}
		if enabledOnly && !jsonx.Truthy(getOr(o, "enabled", true)) {
			continue
		}
		if _, dup := m[id]; !dup {
			order = append(order, id)
		}
		m[id] = o
	}
	return m, order
}

func providerSources(main, config *jsonx.Object) []providerSource {
	graph, err := legacyNode(main)
	if err != nil {
		return nil
	}
	native, err := legacyConfig(config)
	if err != nil {
		return nil
	}
	return sourceProviders(graph, native)
}
