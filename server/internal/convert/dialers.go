package convert

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Ref identifies a profile-local resource in a dependency graph.
type Ref struct{ Kind, ID string }

// Graph stores dependency edges in deterministic discovery order.
type Graph struct {
	keys     []Ref
	children map[Ref][]Ref
}

func newRefGraph() *Graph         { return &Graph{children: map[Ref][]Ref{}} }
func (g *Graph) has(key Ref) bool { _, ok := g.children[key]; return ok }
func (g *Graph) set(key Ref, children []Ref) {
	if !g.has(key) {
		g.keys = append(g.keys, key)
	}
	g.children[key] = children
}

// Keys returns an independent ordered list of graph vertices.
func (g *Graph) Keys() []Ref { return slices.Clone(g.keys) }

// Children returns independent dependency edges for a vertex.
func (g *Graph) Children(key Ref) ([]Ref, bool) {
	children, ok := g.children[key]
	return slices.Clone(children), ok
}

var dialerBuiltinNames = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "PASS-RULE": true, "COMPATIBLE": true}

func stringList(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
func refList(value *[]model.ResourceRef) []model.ResourceRef {
	if value == nil {
		return nil
	}
	return *value
}
func stableRef(ref *model.ResourceRef) bool {
	return ref != nil && (ref.Type == "node" || ref.Type == "group") && strings.TrimSpace(ref.ID) != ""
}
func resourceRef(ref model.ResourceRef) Ref { return Ref{ref.Type, ref.ID} }

type dialerResource struct {
	ref         Ref
	name        string
	enabled     bool
	node        *model.Node
	group       *model.ProxyGroup
	aggregation *model.Aggregation
}
type dialerScope struct {
	keys    []Ref
	objects map[Ref]dialerResource
}

func newDialerScope(config *model.Config) *dialerScope {
	s := &dialerScope{objects: map[Ref]dialerResource{}}
	add := func(value dialerResource) {
		if _, ok := s.objects[value.ref]; !ok {
			s.keys = append(s.keys, value.ref)
		}
		s.objects[value.ref] = value
	}
	if config == nil {
		return s
	}
	for i := range config.Nodes {
		node := &config.Nodes[i]
		add(dialerResource{ref: Ref{"node", node.ID}, name: node.Name, enabled: enabled(node.Enabled), node: node})
	}
	if config.Profile != nil {
		for i := range config.Profile.ProxyGroups {
			group := &config.Profile.ProxyGroups[i]
			add(dialerResource{ref: Ref{"group", group.ID}, name: group.Name, enabled: enabled(group.Enabled), group: group})
		}
	}
	for i := range config.Subscriptions {
		source := &config.Subscriptions[i]
		add(dialerResource{ref: Ref{"subscription", source.ID}, name: source.Name, enabled: enabled(source.Enabled)})
	}
	for i := range config.Aggregations {
		source := &config.Aggregations[i]
		add(dialerResource{ref: Ref{"aggregation", source.ID}, name: source.Name, enabled: enabled(source.Enabled), aggregation: source})
	}
	return s
}
func configGroups(config *model.Config) []model.ProxyGroup {
	if config == nil || config.Profile == nil {
		return nil
	}
	return config.Profile.ProxyGroups
}

// Scalar compatibility does not apply to identity discriminators. Inspect
// retained JSON rather than the model's number-to-text reading of the field.
func identityString(raw json.RawMessage, field string) error {
	var value *string
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return fmt.Errorf(msgIdentityString, field, raw)
	}
	return nil
}

// recordMembers reads the named members that are present, one at a time.
func recordMembers(member func(string) (json.RawMessage, bool, error), names ...string) (map[string]json.RawMessage, error) {
	members := make(map[string]json.RawMessage, len(names))
	for _, name := range names {
		raw, present, err := member(name)
		if err != nil {
			return nil, err
		}
		if present {
			members[name] = raw
		}
	}
	return members, nil
}

// nodeMember and groupMember read single members of a plain record
// (model.EncodeMember), never the whole record (O10 R9).
func nodeMember(node *model.Node) func(string) (json.RawMessage, bool, error) {
	return func(name string) (json.RawMessage, bool, error) { return model.EncodeMember(node, name) }
}

func groupRecordMember(group *model.ProxyGroup) func(string) (json.RawMessage, bool, error) {
	return func(name string) (json.RawMessage, bool, error) { return model.EncodeMember(group, name) }
}

// ConfigShapes validates metadata needed by node and topology generation.
func ConfigShapes(config *model.Config, requireIDs bool) error {
	if config == nil {
		return nil
	}
	// Only the members checked here are encoded, never a whole record (O10 R9).
	for i := range config.Nodes {
		node := &config.Nodes[i]
		members, err := recordMembers(nodeMember(node), "id", "proxy_string")
		if err != nil {
			return err
		}
		_, hasID := members["id"]
		if (requireIDs || hasID) && strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf(msgResourceField, "id")
		}
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf(msgResourceField, "name")
		}
		// params is a Dynamic: valid JSON, present as a member exactly when it
		// is not zero. Look at its first byte instead of encoding it.
		if !reflect.ValueOf(node.Params).IsZero() {
			raw, err := node.Params.MarshalJSON()
			if err != nil {
				return err
			}
			if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
				return errors.New(msgNodeParamsObject)
			}
		}
		if raw, present := members["proxy_string"]; present {
			var value *model.String
			if json.Unmarshal(raw, &value) != nil {
				return errors.New(msgProxyStringType)
			}
		}
	}
	for _, group := range configGroups(config) {
		members, err := recordMembers(groupRecordMember(&group), "id", "type", "manual_nodes", "include_groups", "proxies", "subscriptions", "aggregations", "use", "proxies_order")
		if err != nil {
			return err
		}
		_, hasID := members["id"]
		if (requireIDs || hasID) && strings.TrimSpace(group.ID) == "" {
			return fmt.Errorf(msgResourceField, "id")
		}
		if strings.TrimSpace(group.Name) == "" {
			return fmt.Errorf(msgResourceField, "name")
		}
		if err := GroupMemberShapes(members); err != nil {
			return err
		}
	}
	return nil
}

// GroupMemberShapes checks the JSON shapes of a proxy group's typed members
// (dialer_references.validate_shapes): the type text, the id lists and
// proxies_order. members are the group's members as JSON.
func GroupMemberShapes(members map[string]json.RawMessage) error {
	{
		if raw, present := members["type"]; present {
			if err := identityString(raw, "type"); err != nil {
				return err
			}
		}
		for _, key := range []string{"manual_nodes", "include_groups", "proxies", "subscriptions", "aggregations", "use"} {
			if raw, present := members[key]; present {
				var values []*model.String
				if json.Unmarshal(raw, &values) != nil || values == nil {
					return fmt.Errorf(msgGroupStringList, key)
				}
				for _, value := range values {
					if value == nil || strings.TrimSpace(string(*value)) == "" {
						return fmt.Errorf(msgGroupStringList, key)
					}
				}
			}
		}
		if raw, present := members["proxies_order"]; present {
			var entries []map[string]json.RawMessage
			if json.Unmarshal(raw, &entries) != nil || entries == nil {
				return errors.New(msgGroupOrder)
			}
			for _, entry := range entries {
				raw, present := entry["type"]
				if !present {
					return errors.New(msgGroupOrder)
				}
				if err := identityString(raw, "proxies_order.type"); err != nil {
					return err
				}
				// Resource IDs use the shared lossless text codec; the type
				// discriminator above deliberately remains strict.
				var id *model.String
				if json.Unmarshal(entry["id"], &id) != nil || id == nil {
					return errors.New(msgGroupOrder)
				}
			}
		}
	}
	return nil
}

// GroupRefs returns ordered manual/group candidates, with the two supported builtins.
func GroupRefs(group model.ProxyGroup) []Ref {
	var result []Ref
	if order := refList(group.ProxiesOrder); len(order) > 0 {
		for _, ref := range order {
			switch ref.Type {
			case "node":
				result = append(result, Ref{"node", ref.ID})
			case "strategy":
				result = append(result, Ref{"group", ref.ID})
			}
		}
	} else {
		for _, id := range stringList(group.ManualNodes) {
			result = append(result, Ref{"node", id})
		}
		for _, id := range stringList(group.IncludeGroups) {
			result = append(result, Ref{"group", id})
		}
		if len(result) == 0 && (stringValue(group.Source) == "node" || stringValue(group.Source) == "strategy") {
			kind := "node"
			if *group.Source == "strategy" {
				kind = "group"
			}
			for _, id := range stringList(group.Proxies) {
				result = append(result, Ref{kind, id})
			}
		}
	}
	for i, ref := range result {
		if ref.Kind == "node" && (ref.ID == "DIRECT" || ref.ID == "REJECT") {
			result[i].Kind = "builtin"
		}
	}
	return result
}

// GroupSources resolves logical sources without loading provider payloads.
func GroupSources(config *model.Config, group model.ProxyGroup) ([]Ref, error) {
	return newDialerScope(config).groupSources(group)
}
func (s *dialerScope) groupSources(group model.ProxyGroup) ([]Ref, error) {
	if group.IncludeAll != nil && *group.IncludeAll || group.IncludeAllNative != nil && *group.IncludeAllNative {
		return nil, errors.New(msgDialerIncludeAll)
	}
	if follow := stringValue(group.FollowGroup); follow != "" {
		return []Ref{{"group", follow}}, nil
	}
	result := GroupRefs(group)
	providers := map[string]map[string]bool{"subscription": {}, "aggregation": {}}
	for _, id := range stringList(group.Subscriptions) {
		providers["subscription"][id] = true
	}
	for _, id := range stringList(group.Aggregations) {
		providers["aggregation"][id] = true
	}
	for _, ref := range refList(group.ProxiesOrder) {
		if ids, ok := providers[ref.Type]; ok {
			ids[ref.ID] = true
		}
	}
	for _, name := range stringList(group.Use) {
		var matches []Ref
		for _, ref := range s.keys {
			resource := s.objects[ref]
			if (ref.Kind == "subscription" || ref.Kind == "aggregation") && resource.enabled && resource.name == name {
				matches = append(matches, ref)
			}
		}
		if len(matches) != 1 {
			return nil, errors.New(msgGroupUse)
		}
		providers[matches[0].Kind][matches[0].ID] = true
	}
	for _, kind := range []string{"subscription", "aggregation"} {
		ids := make([]string, 0, len(providers[kind]))
		for id := range providers[kind] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if strings.TrimSpace(id) == "" {
				return nil, errors.New(msgGroupSourceMissing)
			}
			ref := Ref{kind, id}
			resource, ok := s.objects[ref]
			if !ok || !resource.enabled {
				return nil, errors.New(msgGroupSourceMissing)
			}
			result = append(result, ref)
		}
	}
	if len(result) == 0 {
		return nil, errors.New(msgGroupEmpty)
	}
	return result, nil
}

// NodeDialer reads a node's own dialer-proxy without contacting a converter.
func NodeDialer(node model.Node) (*yamlx.Node, error) {
	if node.ProxyString != nil && *node.ProxyString != "" {
		proxy, err := StructuredProxy(*node.ProxyString)
		if err != nil {
			return nil, err
		}
		value := yamlx.Get(proxy, "dialer-proxy")
		if value != nil && value.ShortTag() == "!!null" {
			value = nil
		}
		return value, nil
	}
	params, err := node.Params.Node()
	if err != nil {
		return nil, err
	}
	if params == nil || params.ShortTag() == "!!null" {
		return nil, nil
	}
	if params.Kind != yaml.MappingNode {
		return nil, errors.New(msgNodeParamsObject)
	}
	value := yamlx.Get(params, "dialer-proxy")
	if value != nil && value.ShortTag() == "!!null" {
		value = nil
	}
	return value, nil
}

// NodeDialerTarget resolves a stable reference or an unambiguous raw name.
func NodeDialerTarget(config *model.Config, node model.Node) (*Ref, error) {
	return newDialerScope(config).nodeTarget(node)
}
func (s *dialerScope) nodeTarget(node model.Node) (*Ref, error) {
	if node.DialerRef != nil {
		if !stableRef(node.DialerRef) {
			return nil, errors.New(msgDialerRef)
		}
		ref := resourceRef(*node.DialerRef)
		return &ref, nil
	}
	value, err := NodeDialer(node)
	if err != nil || value == nil {
		return nil, err
	}
	name := scalarText(value)
	if strings.TrimSpace(name) == "" {
		return nil, errors.New(msgRawDialerName)
	}
	if name == "DIRECT" || name == "REJECT" {
		return &Ref{"builtin", name}, nil
	}
	var matches []Ref
	for _, ref := range s.keys {
		resource := s.objects[ref]
		if (ref.Kind == "node" || ref.Kind == "group") && resource.enabled && resource.name == name {
			matches = append(matches, ref)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf(msgRawDialerMissing, name)
	}
	return &matches[0], nil
}
func aggregationRefs(source *model.Aggregation) []Ref {
	var result []Ref
	for _, id := range source.Nodes {
		result = append(result, Ref{"node", id})
	}
	for _, id := range source.Subscriptions {
		result = append(result, Ref{"subscription", id})
	}
	return result
}
func (s *dialerScope) validateChains() error {
	chains := map[string]bool{}
	for _, ref := range s.keys {
		if resource := s.objects[ref]; resource.group != nil && resource.group.Type == "chain" {
			chains[ref.ID] = true
		}
	}
	for _, ref := range s.keys {
		resource := s.objects[ref]
		group := resource.group
		if group == nil {
			continue
		}
		if chains[stringValue(group.FollowGroup)] {
			return errors.New(msgChainFollow)
		}
		if len(chains) > 0 && resource.enabled && group.Type != "chain" {
			for _, child := range GroupRefs(*group) {
				if child.Kind == "group" {
					target, ok := s.objects[child]
					if !ok || !target.enabled {
						return errors.New(msgChainGroupMissing)
					}
				}
			}
		}
	}
	for _, ref := range s.keys {
		resource := s.objects[ref]
		group := resource.group
		if group == nil || group.Type != "chain" {
			continue
		}
		chain := group.Chain
		if chain == nil {
			return errors.New(msgChainShape)
		}
		data, err := chain.MarshalJSON()
		if err != nil {
			return err
		}
		var wire struct {
			Entry json.RawMessage `json:"entry"`
			Exit  json.RawMessage `json:"exit"`
		}
		if err := jsondecode.Unmarshal(data, &wire); err != nil {
			return err
		}
		if len(wire.Entry) == 0 || len(wire.Exit) == 0 {
			return errors.New(msgChainShape)
		}
		for i, endpoint := range []model.ResourceRef{chain.Entry, chain.Exit} {
			if !stableRef(&endpoint) {
				return errors.New(msgChainEndpoint)
			}
			target, ok := s.objects[resourceRef(endpoint)]
			if !ok || ((i == 0 || resource.enabled) && !target.enabled) {
				return errors.New(msgChainMissing)
			}
			if target.node != nil && target.node.SubscriptionID != "" {
				return errors.New(msgChainManual)
			}
		}
		if chain.Entry.Type == "node" && resourceRef(chain.Entry) == resourceRef(chain.Exit) {
			return errors.New(msgChainSame)
		}
		if len(stringList(group.ManualNodes))+len(stringList(group.IncludeGroups))+len(refList(group.ProxiesOrder))+len(stringList(group.Proxies))+len(stringList(group.Subscriptions))+len(stringList(group.Aggregations))+len(stringList(group.Use)) > 0 || stringValue(group.FollowGroup) != "" || group.IncludeAll != nil && *group.IncludeAll || group.IncludeAllNative != nil && *group.IncludeAllNative {
			return errors.New(msgChainSources)
		}
	}
	return nil
}
func (s *dialerScope) exitDependencies(start Ref) ([]Ref, error) {
	graph := newRefGraph()
	var dependencies []Ref
	pending := []Ref{start}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) {
			continue
		}
		if key.Kind == "builtin" {
			return nil, errors.New(msgChainBuiltin)
		}
		resource, ok := s.objects[key]
		if !ok || !resource.enabled {
			return nil, errors.New(msgChainExitMissing)
		}
		var children []Ref
		var err error
		switch {
		case resource.node != nil:
			if resource.node.SubscriptionID != "" {
				return nil, errors.New(msgChainSubscribed)
			}
		case resource.group != nil && resource.group.Type == "chain":
			dependencies = append(dependencies, key)
		case resource.group != nil:
			children, err = s.groupSources(*resource.group)
		case resource.aggregation != nil:
			children = aggregationRefs(resource.aggregation)
			if len(children) == 0 {
				return nil, errors.New(msgChainAggregationEmpty)
			}
		}
		if err != nil {
			return nil, err
		}
		graph.set(key, children)
		pending = append(pending, children...)
	}
	if err := checkCycles(graph); err != nil {
		return nil, err
	}
	return dependencies, nil
}

// BuildDialerGraph validates profile metadata and constructs its active topology.
func BuildDialerGraph(config *model.Config) (*Graph, error) {
	if err := ConfigShapes(config, true); err != nil {
		return nil, err
	}
	scope := newDialerScope(config)
	if err := scope.validateChains(); err != nil {
		return nil, err
	}
	return scope.graph()
}
func (s *dialerScope) graph() (*Graph, error) {
	graph := newRefGraph()
	var pending []Ref
	for _, key := range s.keys {
		resource := s.objects[key]
		if !resource.enabled {
			continue
		}
		switch {
		case resource.node != nil:
			if resource.node.DialerRef != nil {
				pending = append(pending, key)
				continue
			}
			raw, err := NodeDialer(*resource.node)
			if err != nil {
				return nil, err
			}
			if raw != nil {
				pending = append(pending, key)
			}
		case resource.group != nil && resource.group.Type == "chain":
			pending = append(pending, key)
		}
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) || key.Kind == "builtin" {
			continue
		}
		resource, ok := s.objects[key]
		if !ok || !resource.enabled {
			return nil, errors.New(msgDialerMissing)
		}
		var children []Ref
		var err error
		switch {
		case resource.group != nil && resource.group.Type == "chain":
			if resource.group.Chain == nil {
				return nil, errors.New(msgChainShape)
			}
			children = []Ref{resourceRef(resource.group.Chain.Entry)}
			var nested []Ref
			nested, err = s.exitDependencies(resourceRef(resource.group.Chain.Exit))
			children = append(children, nested...)
		case resource.group != nil:
			children, err = s.groupSources(*resource.group)
		case resource.aggregation != nil:
			children = aggregationRefs(resource.aggregation)
			if len(children) == 0 {
				return nil, errors.New(msgDialerAggregationEmpty)
			}
		case resource.node != nil:
			var target *Ref
			target, err = s.nodeTarget(*resource.node)
			if target != nil {
				children = []Ref{*target}
			}
		}
		if err != nil {
			return nil, err
		}
		graph.set(key, children)
		pending = append(pending, children...)
	}
	if err := checkCycles(graph); err != nil {
		return nil, err
	}
	return graph, nil
}

func checkCycles(graph *Graph) error {
	if graph == nil {
		return nil
	}
	type frame struct {
		key     Ref
		exiting bool
	}
	visiting, done := map[Ref]bool{}, map[Ref]bool{}
	for _, root := range graph.keys {
		pending := []frame{{key: root}}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if current.exiting {
				delete(visiting, current.key)
				done[current.key] = true
				continue
			}
			if visiting[current.key] {
				return errors.New(msgDialerCycle)
			}
			if done[current.key] {
				continue
			}
			visiting[current.key] = true
			pending = append(pending, frame{current.key, true})
			children := graph.children[current.key]
			for i := len(children) - 1; i >= 0; i-- {
				pending = append(pending, frame{key: children[i]})
			}
		}
	}
	return nil
}

// ConfigDialers validates active and retained stable-reference metadata.
func ConfigDialers(config *model.Config) error {
	if err := ConfigShapes(config, true); err != nil {
		return err
	}
	s := newDialerScope(config)
	if err := s.validateChains(); err != nil {
		return err
	}
	if config == nil {
		return nil
	}
	active := false
	for _, group := range configGroups(config) {
		active = active || group.Type == "chain"
	}
	if !active {
		for _, node := range config.Nodes {
			if node.DialerRef != nil {
				active = true
				break
			}
			raw, err := NodeDialer(node)
			if err != nil {
				return err
			}
			if raw != nil {
				active = true
				break
			}
		}
	}
	if active {
		names := map[string]bool{}
		for _, kind := range []string{"node", "group"} {
			ids := map[string]bool{}
			var resources []dialerResource
			if kind == "node" {
				for i := range config.Nodes {
					node := &config.Nodes[i]
					resources = append(resources, dialerResource{ref: Ref{kind, node.ID}, name: node.Name, enabled: enabled(node.Enabled)})
				}
			} else {
				for _, group := range configGroups(config) {
					resources = append(resources, dialerResource{ref: Ref{kind, group.ID}, name: group.Name, enabled: enabled(group.Enabled)})
				}
			}
			for _, resource := range resources {
				if ids[resource.ref.ID] {
					return errors.New(msgDialerUniqueID)
				}
				ids[resource.ref.ID] = true
			}
			for _, resource := range resources {
				if resource.enabled {
					if strings.TrimSpace(resource.name) == "" || names[resource.name] || dialerBuiltinNames[resource.name] {
						return errors.New(msgDialerNames)
					}
					names[resource.name] = true
				}
			}
		}
	}
	var pending []Ref
	for _, node := range config.Nodes {
		if node.DialerRef == nil {
			continue
		}
		if !stableRef(node.DialerRef) {
			return errors.New(msgDialerRef)
		}
		ref := resourceRef(*node.DialerRef)
		target, ok := s.objects[ref]
		if !ok || !target.enabled {
			return errors.New(msgDialerRefMissing)
		}
		if ref.Kind == "node" && ref.ID == node.ID {
			return errors.New(msgDialerSelf)
		}
		pending = append(pending, Ref{"node", node.ID})
	}
	graph, err := s.graph()
	if err != nil {
		return err
	}
	for _, group := range configGroups(config) {
		if group.Type == "chain" {
			pending = append(pending, Ref{"group", group.ID})
		}
	}
	roots, seen := map[Ref]bool{}, map[Ref]bool{}
	for _, ref := range pending {
		roots[ref] = true
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[key] || key.Kind == "builtin" {
			continue
		}
		seen[key] = true
		resource, ok := s.objects[key]
		if !ok {
			return errors.New(msgDialerAbsent)
		}
		if resource.node != nil && resource.node.SubscriptionID != "" {
			return errors.New(msgDialerManual)
		}
		if !resource.enabled && !roots[key] {
			return errors.New(msgDialerMissing)
		}
		if graph.has(key) {
			pending = append(pending, graph.children[key]...)
			continue
		}
		switch {
		case resource.node != nil:
			target, err := s.nodeTarget(*resource.node)
			if err != nil {
				return err
			}
			if target != nil {
				pending = append(pending, *target)
			}
		case resource.group != nil && resource.group.Type == "chain":
			if resource.group.Chain == nil {
				return errors.New(msgChainShape)
			}
			pending = append(pending, resourceRef(resource.group.Chain.Entry))
		case resource.group != nil:
			children, err := s.groupSources(*resource.group)
			if err != nil {
				return err
			}
			pending = append(pending, children...)
		}
	}
	return nil
}
