package convert

import (
	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// The shared implementations live in convert, which already depends on subs.
// Registering typed pure callbacks removes subs' old private validator copy
// without introducing an import cycle or round-tripping model.Config.
func init() {
	subs.SetSharedDialers(subs.SharedDialers{
		Validate:       ConfigDialers,
		ValidateChains: func(config *model.Config) error { return newDialerScope(config).validateChains() },
		Shapes:         ConfigShapes,
		Members:        func(group model.ProxyGroup) []subs.DependencyRef { return subscriptionRefs(GroupRefs(group)) },
		Sources: func(config *model.Config, group model.ProxyGroup) ([]subs.DependencyRef, error) {
			refs, err := GroupSources(config, group)
			return subscriptionRefs(refs), err
		},
		NodeTarget: func(config *model.Config, node model.Node) (*subs.DependencyRef, error) {
			ref, err := NodeDialerTarget(config, node)
			if ref == nil {
				return nil, err
			}
			return &subs.DependencyRef{Kind: ref.Kind, ID: ref.ID}, err
		},
		NodeDialer: NodeDialer,
		Resolve:    ResolveNodeDialer,
		Graph: func(config *model.Config) (*subs.DependencyGraph, error) {
			graph, err := BuildDialerGraph(config)
			if err != nil {
				return nil, err
			}
			out := &subs.DependencyGraph{Order: subscriptionRefs(graph.Keys()), Edges: map[subs.DependencyRef][]subs.DependencyRef{}}
			for _, key := range graph.Keys() {
				children, _ := graph.Children(key)
				out.Edges[subs.DependencyRef{Kind: key.Kind, ID: key.ID}] = subscriptionRefs(children)
			}
			return out, nil
		},
		Filter:     filterSubscriptionCandidates,
		Structured: StructuredProxy,
	})
}
func subscriptionRefs(refs []Ref) []subs.DependencyRef {
	out := make([]subs.DependencyRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, subs.DependencyRef{Kind: ref.Kind, ID: ref.ID})
	}
	return out
}
func filterSubscriptionCandidates(values []subs.FilterCandidate, include, exclude, excludeType string) ([]subs.FilterCandidate, error) {
	nodes := make([]*yamlx.Node, 0, len(values))
	originals := make(map[*yamlx.Node]subs.FilterCandidate, len(values))
	for _, value := range values {
		node := yamlx.Map(yamlx.Str("name"), yamlx.Str(value.Name), yamlx.Str("type"), yamlx.Str(value.Type))
		nodes = append(nodes, node)
		originals[node] = value
	}
	selected, err := FilterProxyNodes(nodes, FilterSpec{Include: include, Exclude: exclude, ExcludeType: excludeType})
	if err != nil {
		return nil, err
	}
	out := make([]subs.FilterCandidate, 0, len(selected))
	for _, node := range selected {
		out = append(out, originals[node])
	}
	return out, nil
}
