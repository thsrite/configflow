package subs

// These old helper signatures only transcode the shared native owner. They do
// not retain a second shape validator, dependency graph builder or filter.
import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"strconv"
)

func parseStructuredProxyString(text string) *jsonx.Object {
	ops := sharedDialers()
	if ops.Structured == nil {
		return nil
	}
	node, err := ops.Structured(text)
	if err != nil || node == nil {
		return nil
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	out, _ := value.(*jsonx.Object)
	return out
}
func rawDialer(value *jsonx.Object) (any, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.NodeDialer == nil {
		return nil, &CollaborationError{Operation: "node dialer"}
	}
	target, err := ops.NodeDialer(node)
	if err != nil {
		return nil, referenceFailure(err)
	}
	if target == nil {
		return nil, nil
	}
	data, err := yamlx.ToJSON(target)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func validateShapes(value *jsonx.Object, ids bool) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	ops := sharedDialers()
	if ops.Shapes == nil {
		return &CollaborationError{Operation: "resource shapes"}
	}
	return referenceFailure(ops.Shapes(config, ids))
}

// ValidateDialers adapts one view to the sole native dependency validator.
func ValidateDialers(value *jsonx.Object) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	ops := sharedDialers()
	if ops.Validate == nil {
		return &CollaborationError{Operation: "dialer validation"}
	}
	return referenceFailure(ops.Validate(config))
}

type ref struct{ kind, id string }

func legacyRefs(values []DependencyRef) []ref {
	out := make([]ref, 0, len(values))
	for _, value := range values {
		out = append(out, ref{value.Kind, value.ID})
	}
	return out
}
func groupMembers(value *jsonx.Object) []ref {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return nil
	}
	ops := sharedDialers()
	if ops.Members == nil {
		return nil
	}
	return legacyRefs(ops.Members(group))
}
func groupDependencies(configValue, groupValue *jsonx.Object) ([]ref, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return nil, err
	}
	group, err := decodeLegacy[model.ProxyGroup](groupValue)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.Sources == nil {
		return nil, &CollaborationError{Operation: "group sources"}
	}
	refs, err := ops.Sources(config, group)
	return legacyRefs(refs), referenceFailure(err)
}
func dialerTarget(configValue, nodeValue *jsonx.Object) (*ref, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return nil, err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.NodeTarget == nil {
		return nil, &CollaborationError{Operation: "dialer target"}
	}
	target, err := ops.NodeTarget(config, node)
	if target == nil {
		return nil, referenceFailure(err)
	}
	return &ref{target.Kind, target.ID}, referenceFailure(err)
}

type depGraph struct {
	order []ref
	m     map[ref][]ref
}

func dependencyGraph(value *jsonx.Object) (*depGraph, error) {
	config, err := legacyConfig(value)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.Graph == nil {
		return nil, &CollaborationError{Operation: "dialer graph"}
	}
	graph, err := ops.Graph(config)
	if err != nil {
		return nil, referenceFailure(err)
	}
	if graph == nil {
		return nil, &CollaborationError{Operation: "dialer graph result"}
	}
	out := &depGraph{order: legacyRefs(graph.Order), m: map[ref][]ref{}}
	for key, children := range graph.Edges {
		out.m[ref{key.Kind, key.ID}] = legacyRefs(children)
	}
	return out, nil
}
func resolveDialer(configValue, nodeValue *jsonx.Object) (string, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return "", err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return "", err
	}
	ops := sharedDialers()
	if ops.Resolve == nil {
		return "", &CollaborationError{Operation: "dialer resolution"}
	}
	name, err := ops.Resolve(config, node)
	if name == nil {
		return "", referenceFailure(err)
	}
	return *name, referenceFailure(err)
}

// Candidate is the old filter boundary. Raw is never inspected by the native owner.
type Candidate struct {
	Name, Type string
	Raw        any
}

func filterProviderCandidates(values []Candidate, include, exclude, excludeType string) ([]Candidate, error) {
	ops := sharedDialers()
	if ops.Filter == nil {
		return nil, &CollaborationError{Operation: "provider filtering"}
	}
	native := make([]FilterCandidate, 0, len(values))
	for index, value := range values {
		native = append(native, FilterCandidate{Name: value.Name, Type: value.Type, Key: strconv.Itoa(index)})
	}
	selected, err := ops.Filter(native, include, exclude, excludeType)
	if err != nil {
		return nil, referenceFailure(err)
	}
	out := make([]Candidate, 0, len(selected))
	for _, value := range selected {
		index, err := strconv.Atoi(value.Key)
		if err != nil {
			return nil, err
		}
		if index < 0 || index >= len(values) {
			return nil, &CollaborationError{Operation: "provider filtering result"}
		}
		out = append(out, values[index])
	}
	return out, nil
}
