package convert

import (
	"configflow/internal/jsondecode"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

var mihomoLog = logx.Get("backend.converters.mihomo")

// FixProxyNode fills fields required by Mihomo's VLESS implementation.
func FixProxyNode(proxy *yamlx.Node) {
	if scalarText(yamlx.Get(proxy, "type")) != "vless" {
		return
	}
	encryption := yamlx.Resolve(yamlx.Get(proxy, "encryption"))
	if encryption == nil || encryption.ShortTag() == "!!null" || scalarText(encryption) == "" || scalarText(encryption) == "zero" {
		// Non-string encryption is not a missing value.
		if encryption == nil || encryption.ShortTag() == "!!null" || encryption.ShortTag() == "!!str" {
			yamlx.Set(proxy, "encryption", yamlx.Str("none"))
		}
	}
	reality := yamlx.Resolve(yamlx.Get(proxy, "reality-opts"))
	if reality != nil && reality.Kind == yaml.MappingNode && yamlx.Get(reality, "short-id") == nil {
		yamlx.Set(reality, "short-id", yamlx.Str(""))
	}
}

func scalarText(node *yamlx.Node) string {
	node = yamlx.Resolve(node)
	if node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		return node.Value
	}
	return ""
}

var githubDomains = map[string]bool{"github.com": true, "raw.githubusercontent.com": true, "gist.githubusercontent.com": true, "api.github.com": true}

// GitHubURL applies the configured GitHub download proxy to supported hosts.
func GitHubURL(address string, config *model.Config) (string, error) {
	if address == "" || config == nil {
		return address, nil
	}
	proxy := strings.TrimSpace(config.SystemConfig.GitHubProxyDomain)
	if proxy == "" {
		return address, nil
	}
	parsed, err := weburl.Parse(address)
	if err != nil {
		if strings.ContainsAny(address, "[]") && !strings.Contains(address, "[v") {
			return "", err
		}
		return address, nil
	}
	if !githubDomains[weburl.Hostname(parsed)] {
		return address, nil
	}
	if !strings.HasPrefix(proxy, "http://") && !strings.HasPrefix(proxy, "https://") {
		proxy = "https://" + proxy
	}
	if !strings.HasSuffix(proxy, "/") {
		proxy += "/"
	}
	if strings.HasPrefix(address, proxy) {
		return address, nil
	}
	return proxy + address, nil
}

// Region probe names are shared with the generated Mihomo listener.
const (
	RegionProbeGroup       = "ConfigFlow-Region-Probe"
	RegionProbeListener    = "configflow-region-probe"
	DefaultRegionProbePort = 17999
)

// InboundPortKeys lists Mihomo's inbound port settings in configuration order.
var InboundPortKeys = []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"}

// ListeningPort identifies a port and the setting that owns it.
type ListeningPort struct {
	Port  int64
	Owner string
}

func integerPort(node *yamlx.Node) (int64, bool) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
		return 0, false
	}
	var value int64
	if err := yamlx.Decode(node, &value); err != nil {
		return 0, false
	}
	return value, true
}

// UsedPorts returns integer inbound ports, excluding the region probe listener.
func UsedPorts(document *yamlx.Node) map[int64]bool {
	used := map[int64]bool{}
	for _, key := range InboundPortKeys {
		if port, ok := integerPort(yamlx.Get(document, key)); ok {
			used[port] = true
		}
	}
	listeners := yamlx.Resolve(yamlx.Get(document, "listeners"))
	if listeners != nil && listeners.Kind == yaml.SequenceNode {
		for _, listener := range listeners.Content {
			if scalarText(yamlx.Get(listener, "name")) == RegionProbeListener {
				continue
			}
			if port, ok := integerPort(yamlx.Get(listener, "port")); ok {
				used[port] = true
			}
		}
	}
	return used
}

// ListenPorts returns inbound owners, remaining listener ports in numeric order,
// and the external controller. A duplicate retains its first position.
func ListenPorts(document *yamlx.Node) []ListeningPort {
	var result []ListeningPort
	index := map[int64]int{}
	set := func(port int64, owner string, replace bool) {
		if i, ok := index[port]; ok {
			if replace {
				result[i].Owner = owner
			}
			return
		}
		index[port] = len(result)
		result = append(result, ListeningPort{port, owner})
	}
	for _, key := range InboundPortKeys {
		if port, ok := integerPort(yamlx.Get(document, key)); ok {
			set(port, key, true)
		}
	}
	var ports []int64
	for port := range UsedPorts(document) {
		ports = append(ports, port)
	}
	slices.Sort(ports)
	for _, port := range ports {
		set(port, "listeners", false)
	}
	controller := scalarText(yamlx.Get(document, "external-controller"))
	_, portText, err := net.SplitHostPort(controller)
	if err == nil {
		if port, err := strconv.ParseUint(portText, 10, 63); err == nil {
			set(int64(port), "external-controller", false)
		}
	}
	return result
}

var (
	proxyYAMLCacheSize      = 128
	proxyYAMLMaxInputBytes  = 64 * 1024
	proxyYAMLMaxResultBytes = 64 * 1024
	proxyYAMLMaxObjects     = 2048
	proxyYAMLMaxDepth       = 32
	proxyYAMLMu             sync.Mutex
	proxyYAMLOrder          = list.New()
	proxyYAMLIndex          = map[string]*list.Element{}
	parseProxyYAML          = func(text string) (*yamlx.Node, error) { return yamlx.Parse([]byte(text)) }
)

type proxyYAMLEntry struct {
	text string
	data []byte
}

// cacheableProxyNode bounds retained trees. Shared/alias graphs bypass this
// copying cache entirely; the reader's independent expansion budget still runs.
func cacheableProxyNode(root *yamlx.Node, encoded []byte) bool {
	if len(encoded) > proxyYAMLMaxResultBytes {
		return false
	}
	type visit struct {
		node  *yamlx.Node
		depth int
	}
	pending := []visit{{root, 0}}
	seen := map[*yamlx.Node]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.depth > proxyYAMLMaxDepth || seen[current.node] || current.node.Kind == yaml.AliasNode {
			return false
		}
		seen[current.node] = true
		if len(seen) > proxyYAMLMaxObjects {
			return false
		}
		for _, child := range current.node.Content {
			pending = append(pending, visit{child, current.depth + 1})
		}
	}
	return true
}

func loadProxyNode(text string) (*yamlx.Node, error) {
	if len(text) > proxyYAMLMaxInputBytes {
		return readProxyNode(text)
	}
	proxyYAMLMu.Lock()
	if element, ok := proxyYAMLIndex[text]; ok {
		proxyYAMLOrder.MoveToBack(element)
		data := element.Value.(*proxyYAMLEntry).data
		proxyYAMLMu.Unlock()
		return yamlx.Parse(data)
	}
	node, err := readProxyNode(text)
	if err != nil {
		proxyYAMLMu.Unlock()
		return nil, err
	}
	if node == nil {
		proxyYAMLMu.Unlock()
		return nil, nil
	}
	encoded, err := yamlx.Marshal(node)
	if err != nil {
		proxyYAMLMu.Unlock()
		return nil, err
	}
	if cacheableProxyNode(node, encoded) && scalarText(yamlx.Get(node, "type")) != "" && proxyYAMLCacheSize > 0 {
		proxyYAMLIndex[text] = proxyYAMLOrder.PushBack(&proxyYAMLEntry{text, encoded})
		for proxyYAMLOrder.Len() > proxyYAMLCacheSize {
			oldest := proxyYAMLOrder.Front()
			proxyYAMLOrder.Remove(oldest)
			delete(proxyYAMLIndex, oldest.Value.(*proxyYAMLEntry).text)
		}
	}
	proxyYAMLMu.Unlock()
	return node, nil
}

// StructuredProxy parses JSON first (including surrogate pairs), then YAML.
// Ordinary YAML syntax failures allow a URI parser to try the text. Policy,
// recursion and expansion-budget errors are returned without another parser.
func StructuredProxy(text string) (*yamlx.Node, error) {
	text = strings.TrimSpace(text)
	first, _, _ := strings.Cut(text, "\n")
	if scheme, _, found := strings.Cut(first, "://"); found && !strings.ContainsAny(scheme, " :") {
		return nil, nil
	}
	if strings.HasPrefix(text, "{") && json.Valid([]byte(text)) {
		node, err := yamlx.FromJSON([]byte(text))
		if err != nil {
			return nil, err
		}
		return proxyMapping(node)
	}
	node, err := loadProxyNode(text)
	if err != nil {
		if errors.Is(err, yamlx.ErrSyntax) {
			return nil, nil
		}
		return nil, &ProxyYAMLError{Cause: err}
	}
	return proxyMapping(node)
}

func proxyMapping(node *yamlx.Node) (*yamlx.Node, error) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	typeNode := yamlx.Resolve(yamlx.Get(node, "type"))
	if typeNode == nil || typeNode.ShortTag() == "!!null" || scalarText(typeNode) == "" {
		if typeNode != nil && typeNode.ShortTag() != "!!str" && typeNode.ShortTag() != "!!null" {
			return nil, &ProxyYAMLError{Cause: errors.New(msgProxyTypeString)}
		}
		return nil, nil
	}
	return node, nil
}

// NodeConverter converts opaque URI/base64 proxy strings through a collaborator.
type NodeConverter interface {
	ConvertProxy(context.Context, string) (*yamlx.Node, error)
}

// Conversions records private immutable serialized nodes, including failed
// conversions. Each Get parses a new owned graph; no caller can poison a cache.
type Conversions struct {
	mu     sync.Mutex
	keys   []string
	values map[string][]byte
}

// NewConversions returns an empty conversion record.
func NewConversions() *Conversions { return &Conversions{values: map[string][]byte{}} }

// Get returns an independent recorded node; found distinguishes a failed result.
func (c *Conversions) Get(text string) (node *yamlx.Node, found bool, err error) {
	c.mu.Lock()
	data, found := c.values[text]
	c.mu.Unlock()
	if !found || data == nil {
		return nil, found, nil
	}
	node, err = yamlx.Parse(data)
	return node, true, err
}

// Keys lists recorded strings in first-seen order.
func (c *Conversions) Keys() []string { c.mu.Lock(); defer c.mu.Unlock(); return slices.Clone(c.keys) }

// Len returns the number of recorded conversion attempts.
func (c *Conversions) Len() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.values) }

func (c *Conversions) record(text string, node *yamlx.Node) error {
	var data []byte
	if node != nil {
		var err error
		data, err = yamlx.Marshal(node)
		if err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = map[string][]byte{}
	}
	if _, ok := c.values[text]; !ok {
		c.keys = append(c.keys, text)
	}
	c.values[text] = data
	return nil
}

type conversionScope struct {
	results *Conversions
	reuse   bool
}
type conversionScopeKey struct{}

// WithConversions records conversions in this context's delivery scope.
func WithConversions(ctx context.Context, results *Conversions, reuse bool) context.Context {
	return context.WithValue(ctx, conversionScopeKey{}, &conversionScope{results, reuse})
}

// EnsureConversions reuses the current delivery record or creates one.
func EnsureConversions(ctx context.Context) context.Context {
	if scope, _ := ctx.Value(conversionScopeKey{}).(*conversionScope); scope != nil && scope.results != nil {
		return ctx
	}
	return WithConversions(ctx, NewConversions(), true)
}

// NodeProxy converts one typed stored node to an owned Mihomo proxy graph.
func NodeProxy(ctx context.Context, node model.Node, converter NodeConverter) (*yamlx.Node, error) {
	if node.ProxyString != nil && *node.ProxyString != "" {
		text := *node.ProxyString
		proxy, err := StructuredProxy(text)
		if err != nil {
			return nil, err
		}
		if proxy != nil {
			mihomoLog.Info(fmt.Sprintf(msgProxyLocalLog, node.Name))
		}
		if proxy == nil {
			scope, _ := ctx.Value(conversionScopeKey{}).(*conversionScope)
			found := false
			if scope != nil && scope.reuse && scope.results != nil {
				proxy, found, err = scope.results.Get(text)
				if err != nil {
					return nil, err
				}
			}
			if !found {
				if converter == nil {
					return nil, errors.New(msgProxyConverterMissing)
				}
				mihomoLog.Info(fmt.Sprintf(msgProxyRemoteLog, node.Name))
				proxy, err = converter.ConvertProxy(ctx, text)
				if err != nil {
					return nil, err
				}
				if proxy != nil {
					if err := ValidateProxyGraph(proxy); err != nil {
						return nil, err
					}
				}
				if scope != nil && scope.results != nil {
					if err := scope.results.record(text, proxy); err != nil {
						return nil, err
					}
				}
			}
		}
		if proxy == nil || len(proxy.Content) == 0 {
			return nil, nil
		}
		if proxy.Kind != yaml.MappingNode {
			return nil, errors.New(msgProxyYAMLNode)
		}
		yamlx.Set(proxy, "name", yamlx.Str(node.Name))
		FixProxyNode(proxy)
		if err := ValidateProxyGraph(proxy); err != nil {
			return nil, err
		}
		return proxy, nil
	}
	// A proxy_string owns its protocol type. Only the stored-node path uses
	// Node.Type, whose original JSON must remain a string discriminator.
	data, err := model.Marshal(node)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Type json.RawMessage `json:"type"`
	}
	if err := jsondecode.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	if metadata.Type != nil {
		if err := identityString(metadata.Type, msgNodeTypeField); err != nil {
			return nil, err
		}
	}
	kind := cases.Lower(language.Und).String(node.Type)
	if kind == "" {
		return nil, nil
	}
	params, err := node.Params.Node()
	if err != nil {
		return nil, err
	}
	if params.ShortTag() != "!!null" && params.Kind != yaml.MappingNode {
		return nil, errors.New(msgNodeParamsObject)
	}
	// R2 retains valid integer notation from storage (e.g. 443.0). An R5
	// invalid field uses its typed zero, never the preserved invalid bytes.
	// Quoted integers are accepted on read but generated as integer scalars.
	port := yamlx.Int(int64(node.Port))
	if !model.IssueUnder(model.Issues(node), "port") {
		record, err := yamlx.FromJSON(data)
		if err != nil {
			return nil, err
		}
		if original := yamlx.Get(record, "port"); original != nil && (original.ShortTag() == "!!int" || original.ShortTag() == "!!float") {
			port = original
		}
	}
	proxy := yamlx.Map(yamlx.Str("name"), yamlx.Str(node.Name), yamlx.Str("type"), yamlx.Str(kind), yamlx.Str("server"), yamlx.Str(node.Server), yamlx.Str("port"), port)
	for name, value := range yamlx.Pairs(params) {
		yamlx.Set(proxy, name, value)
	}
	FixProxyNode(proxy)
	if err := ValidateProxyGraph(proxy); err != nil {
		return nil, err
	}
	return proxy, nil
}

// ValidateProxyGraph applies the shared policy before a collaborator's graph
// enters typed conversion. It does not flatten aliases into JSON occurrences.
func ValidateProxyGraph(node *yamlx.Node) error {
	_, err := yamlx.Encode(node)
	if err != nil {
		return fmt.Errorf("%s: %w", msgProxyYAMLInvalid, err)
	}
	return nil
}

type optionalNodeConverter struct{ source NodeConverter }

// OptionalNodeConverter preserves the generation policy for optional remote
// conversion: unavailable nodes are skipped, but Runtime shutdown always wins.
func OptionalNodeConverter(source NodeConverter) NodeConverter {
	return optionalNodeConverter{source: source}
}
func (c optionalNodeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.source == nil {
		return nil, nil
	}
	node, err := c.source.ConvertProxy(ctx, text)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, nil
	}
	return node, nil
}
