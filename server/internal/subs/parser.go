package subs

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// ParsedProxy contains one typed URI node or an open engine object. Raw is nil
// for URI nodes; a structured object's physical graph remains independently owned.
type ParsedProxy struct {
	Node model.Node
	Raw  *yamlx.Node
}

// ParseList reads subscription share links and structured engine objects.
// Invalid individual entries are skipped; valid entries retain source order.
func ParseList(content string) []ParsedProxy {
	content = strings.TrimSpace(content)
	head := textPrefix(content, 50)
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "name:") || strings.HasPrefix(content, "type:") || strings.Contains(head, "name:") {
		if object := ParseObject(content); object != nil {
			if parsed, err := parsedObject(object); err == nil {
				return []ParsedProxy{parsed}
			}
		}
	}
	if strings.HasPrefix(content, "- name:") || strings.HasPrefix(content, "- type:") {
		graph, err := subscriptionGraph(content)
		if err == nil && graph.Kind == yaml.SequenceNode {
			result := []ParsedProxy{}
			for _, item := range graph.Content {
				resolved := yamlx.Resolve(item)
				if resolved != nil && resolved.Kind == yaml.MappingNode {
					if _, err := yamlx.ToJSON(item); err != nil {
						continue
					}
					if parsed, err := parsedObject(yamlx.CopyGraph(item)); err == nil {
						result = append(result, parsed)
					}
				}
			}
			return result
		}
	}
	result := []ParsedProxy{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			line = strings.TrimSpace(line[2:])
		}
		var node *model.Node
		var err error
		switch {
		case strings.HasPrefix(line, "ss://"):
			node, err = parseSS(line)
		case strings.HasPrefix(line, "ssr://"):
			node, err = parseSSR(line)
		case strings.HasPrefix(line, "vmess://"):
			node, err = parseVMessNode(line)
		case strings.HasPrefix(line, "vless://"):
			node, err = parseVlessNode(line)
		case strings.HasPrefix(line, "trojan://"):
			node, err = parseTrojanNode(line)
		case strings.HasPrefix(line, "hysteria://") || strings.HasPrefix(line, "hysteria2://"):
			node, err = parseHysteriaNode(line)
		case strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}"):
			if object := ParseObject(line); object != nil {
				if parsed, err := parsedObject(object); err == nil {
					result = append(result, parsed)
				}
			}
		}
		if err == nil && node != nil {
			result = append(result, ParsedProxy{Node: *node})
		}
	}
	return result
}

// ParseObject reads an open JSON/YAML mapping with the shared graph policy.
func ParseObject(source string) *yamlx.Node {
	graph, err := subscriptionGraph(source)
	if err != nil || graph.Kind != yaml.MappingNode {
		return nil
	}
	return graph
}

func subscriptionGraph(source string) (*yamlx.Node, error) {
	var graph *yamlx.Node
	var err error
	if json.Valid([]byte(source)) {
		graph, err = yamlx.FromJSON([]byte(source))
	} else {
		graph, err = yamlx.Parse([]byte(source))
	}
	if err != nil {
		return nil, err
	}
	if err := yamlx.ProjectJSONKeys(graph, yamlx.RejectNonStringKeys); err != nil {
		return nil, err
	}
	// Validate the entire selected graph before copying owned nodes.
	if _, err := yamlx.ToJSON(graph); err != nil {
		return nil, err
	}
	return yamlx.Resolve(graph), nil
}
func parsedObject(graph *yamlx.Node) (ParsedProxy, error) {
	data, err := yamlx.ToJSON(graph)
	if err != nil {
		return ParsedProxy{}, err
	}
	var node model.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return ParsedProxy{}, err
	}
	return ParsedProxy{Node: node, Raw: graph}, nil
}

func textPrefix(text string, count int) string {
	n := 0
	for i := range text {
		if n == count {
			return text[:i]
		}
		n++
	}
	return text
}

func makeNode(name, kind, server string, port model.Int, params *yamlx.Node) (*model.Node, error) {
	var dynamic model.Dynamic
	if err := dynamic.SetNode(params); err != nil {
		return nil, err
	}
	// Materialize the declared zero fields too, preserving the stored node shape.
	data, err := jsonresp.Encode(struct {
		Name   string        `json:"name"`
		Type   string        `json:"type"`
		Server string        `json:"server"`
		Port   model.Int     `json:"port"`
		Params model.Dynamic `json:"params"`
	}{name, kind, server, port, dynamic})
	if err != nil {
		return nil, err
	}
	var node model.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// parseURIPort parses the raw URI protocol token. Structured JSON fields use
// the shared model.Int codec instead; URI tokens retain ordinary Go signs and
// surrounding whitespace accepted by existing subscription sources.
func parseURIPort(text string) (model.Int, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	return model.Int(value), err
}
func splitPair(text, sep string) (string, string, error) {
	a, b, ok := strings.Cut(text, sep)
	if !ok {
		return "", "", errShareLink
	}
	return a, b, nil
}

var errShareLink = errors.New("invalid subscription share link")

func fragment(text, fallback string) (string, string) {
	if uri, name, ok := strings.Cut(text, "#"); ok {
		return uri, name
	}
	return text, fallback
}

func parseSS(uri string) (*model.Node, error) {
	uri = strings.TrimPrefix(uri, "ss://")
	if !strings.Contains(uri, "@") {
		return nil, nil
	}
	uri, name := fragment(uri, "SS Node")
	var method, password, serverInfo string
	if strings.Count(uri, "@") == 1 {
		user, server, _ := strings.Cut(uri, "@")
		serverInfo = server
		decoded, err := decodeSubscriptionText(user)
		if err == nil {
			method, password, _ = strings.Cut(decoded, ":")
		}
		if err != nil || !strings.Contains(decoded, ":") {
			var err error
			method, password, err = splitPair(user, ":")
			if err != nil {
				return nil, err
			}
		}
	} else {
		decoded, err := decodeSubscriptionText(uri)
		if err != nil {
			return nil, err
		}
		user, server, err := splitPair(decoded, "@")
		if err != nil {
			return nil, err
		}
		serverInfo = server
		method, password, err = splitPair(user, ":")
		if err != nil {
			return nil, err
		}
		serverInfo, name = fragment(serverInfo, name)
	}
	server, port, err := splitPair(serverInfo, ":")
	if err != nil {
		return nil, err
	}
	number, err := parseURIPort(port)
	if err != nil {
		return nil, err
	}
	return makeNode(name, "ss", server, number, yamlx.Map(yamlx.Str("cipher"), yamlx.Str(method), yamlx.Str("password"), yamlx.Str(password)))
}
func parseSSR(uri string) (*model.Node, error) {
	decoded, err := decodeSubscriptionText(strings.TrimPrefix(uri, "ssr://"))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(decoded, "/")
	fields := strings.SplitN(parts[0], ":", 6)
	if len(fields) != 6 {
		return nil, errShareLink
	}
	password, err := decodeSubscriptionText(fields[5])
	if err != nil {
		return nil, err
	}
	options := map[string]string{}
	if len(parts) > 1 {
		if _, query, ok := strings.Cut(parts[1], "?"); ok {
			for _, item := range strings.Split(query, "&") {
				if key, value, ok := strings.Cut(item, "="); ok {
					decoded, err := decodeSubscriptionText(value)
					if err != nil {
						return nil, err
					}
					options[key] = decoded
				}
			}
		}
	}
	pick := func(key, fallback string) string {
		if value, ok := options[key]; ok {
			return value
		}
		return fallback
	}
	port, err := parseURIPort(fields[1])
	if err != nil {
		return nil, err
	}
	return makeNode(pick("remarks", "SSR Node"), "ssr", fields[0], port, yamlx.Map(
		yamlx.Str("cipher"), yamlx.Str(fields[3]), yamlx.Str("password"), yamlx.Str(password),
		yamlx.Str("protocol"), yamlx.Str(fields[2]), yamlx.Str("obfs"), yamlx.Str(fields[4]),
		yamlx.Str("protocol-param"), yamlx.Str(pick("protoparam", "")), yamlx.Str("obfs-param"), yamlx.Str(pick("obfsparam", ""))))
}

// vmessWire selects only protocol fields using standard JSON last-member-wins
// semantics. Its bounded projection then enters the shared model codec; no
// private scalar decoding or whole-config projection is needed per node.
type vmessWire struct {
	Name    json.RawMessage `json:"ps"`
	Server  json.RawMessage `json:"add"`
	Port    json.RawMessage `json:"port"`
	UUID    json.RawMessage `json:"id"`
	AlterID json.RawMessage `json:"aid"`
	Cipher  json.RawMessage `json:"scy"`
	Network json.RawMessage `json:"net"`
	TLS     json.RawMessage `json:"tls"`
	Path    json.RawMessage `json:"path"`
	Host    json.RawMessage `json:"host"`
}

type vmessFields struct {
	Name    *string   `json:"ps"`
	Server  string    `json:"add"`
	Port    model.Int `json:"port"`
	AlterID model.Int `json:"aid"`
	Fields  model.Fields
	Raw     model.Raw
}

func (r *vmessFields) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }
func (r vmessFields) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&r, &r.Raw) }

func wireValue(raw json.RawMessage, fallback *yamlx.Node) (*yamlx.Node, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	return yamlx.FromJSON(raw)
}
func parseVMessNode(uri string) (*model.Node, error) {
	decoded, err := decodeSubscriptionText(strings.TrimPrefix(uri, "vmess://"))
	if err != nil {
		return nil, err
	}
	payload := bytes.TrimSpace([]byte(decoded))
	if len(payload) == 0 || payload[0] != '{' {
		return nil, errShareLink
	}
	var wire vmessWire
	if err := jsondecode.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire.Port) == 0 {
		wire.Port = json.RawMessage("0")
	}
	if len(wire.AlterID) == 0 {
		wire.AlterID = json.RawMessage("0")
	}
	projected, err := jsonresp.Encode(wire)
	if err != nil {
		return nil, err
	}
	var source vmessFields
	if err := json.Unmarshal(projected, &source); err != nil {
		return nil, err
	}
	if issues := model.Issues(&source); model.IssueUnder(issues, "port") || model.IssueUnder(issues, "aid") {
		return nil, errShareLink
	}
	params := yamlx.Map()
	for _, field := range []struct {
		key      string
		raw      json.RawMessage
		fallback *yamlx.Node
	}{
		{"uuid", wire.UUID, yamlx.Str("")}, {"cipher", wire.Cipher, yamlx.Str("auto")}, {"network", wire.Network, yamlx.Str("tcp")},
	} {
		value, err := wireValue(field.raw, field.fallback)
		if err != nil {
			return nil, err
		}
		yamlx.Set(params, field.key, value)
	}
	// Only these two protocol numbers are normalized. Opaque engine parameters
	// keep their source tags; downstream typed clients own their option schema.
	ordered := yamlx.Map(yamlx.Str("uuid"), yamlx.Get(params, "uuid"), yamlx.Str("alterId"), yamlx.Int(int64(source.AlterID)),
		yamlx.Str("cipher"), yamlx.Get(params, "cipher"), yamlx.Str("network"), yamlx.Get(params, "network"))
	tls, err := wireValue(wire.TLS, yamlx.Str(""))
	if err != nil {
		return nil, err
	}
	yamlx.Set(ordered, "tls", yamlx.Bool(tls.Kind == yaml.ScalarNode && tls.ShortTag() == "!!str" && tls.Value == "tls"))
	ws := yamlx.Map()
	network := yamlx.Get(params, "network")
	if network.Kind == yaml.ScalarNode && network.ShortTag() == "!!str" && network.Value == "ws" {
		path, err := wireValue(wire.Path, yamlx.Str("/"))
		if err != nil {
			return nil, err
		}
		host, err := wireValue(wire.Host, yamlx.Str(""))
		if err != nil {
			return nil, err
		}
		ws = yamlx.Map(yamlx.Str("path"), path, yamlx.Str("headers"), yamlx.Map(yamlx.Str("Host"), host))
	}
	yamlx.Set(ordered, "ws-opts", ws)
	var dynamic model.Dynamic
	if err := dynamic.SetNode(ordered); err != nil {
		return nil, err
	}
	// Preserve original type evidence for declared Node text fields, so client
	// validation can fail closed with the stored value and caller's owner ID.
	nameText := "VMess Node"
	if source.Name != nil {
		nameText = *source.Name
	}
	name, err := jsonresp.Encode(nameText)
	if err != nil {
		return nil, err
	}
	server, err := jsonresp.Encode(source.Server)
	if err != nil {
		return nil, err
	}
	issues := model.Issues(&source)
	if model.IssueUnder(issues, "ps") {
		name = wire.Name
	}
	if model.IssueUnder(issues, "add") {
		server = wire.Server
	}
	data, err := jsonresp.Encode(struct {
		Name   json.RawMessage `json:"name"`
		Type   string          `json:"type"`
		Server json.RawMessage `json:"server"`
		Port   model.Int       `json:"port"`
		Params model.Dynamic   `json:"params"`
	}{name, "vmess", server, source.Port, dynamic})
	if err != nil {
		return nil, err
	}
	var node model.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

type queryOptions map[string][]string

func queryValues(query string) queryOptions {
	_, values := ParseQueryOptions(query)
	return queryOptions(values)
}
func (q queryOptions) one(key string) (string, bool) {
	values, ok := q[key]
	if !ok || len(values) != 1 {
		return "", false
	}
	return values[0], true
}
func (q queryOptions) value(key, fallback string) *yamlx.Node {
	values, ok := q[key]
	if !ok {
		return yamlx.Str(fallback)
	}
	if len(values) == 1 {
		return yamlx.Str(values[0])
	}
	items := make([]*yamlx.Node, 0, len(values))
	for _, value := range values {
		items = append(items, yamlx.Str(value))
	}
	return yamlx.Seq(items...)
}
func parseVlessNode(uri string) (*model.Node, error) {
	uri, name := fragment(strings.TrimPrefix(uri, "vless://"), "VLESS Node")
	uuid, serverInfo, ok := strings.Cut(uri, "@")
	if !ok {
		return nil, nil
	}
	serverPort := serverInfo
	options := queryOptions{}
	if address, query, ok := strings.Cut(serverInfo, "?"); ok {
		serverPort = address
		options = queryValues(query)
	}
	at := strings.LastIndex(serverPort, ":")
	if at < 0 {
		return nil, nil
	}
	port, err := parseURIPort(serverPort[at+1:])
	if err != nil {
		return nil, err
	}
	params := yamlx.Map(yamlx.Str("uuid"), yamlx.Str(uuid), yamlx.Str("encryption"), options.value("encryption", "none"), yamlx.Str("security"), options.value("security", "none"), yamlx.Str("type"), options.value("type", "tcp"))
	security, _ := options.one("security")
	if security == "tls" || security == "reality" {
		yamlx.Set(params, "sni", options.value("sni", ""))
		yamlx.Set(params, "fp", options.value("fp", ""))
	}
	if security == "reality" {
		for _, key := range []string{"pbk", "sid", "spx"} {
			yamlx.Set(params, key, options.value(key, ""))
		}
	}
	if security == "tls" {
		if values := options["ech"]; len(values) > 0 {
			yamlx.Set(params, "ech", options.value("ech", ""))
		}
	}
	kind, _ := options.one("type")
	switch kind {
	case "ws":
		yamlx.Set(params, "ws-opts", yamlx.Map(yamlx.Str("path"), options.value("path", "/"), yamlx.Str("headers"), yamlx.Map(yamlx.Str("Host"), options.value("host", ""))))
	case "grpc":
		yamlx.Set(params, "grpc-opts", yamlx.Map(yamlx.Str("grpc-service-name"), options.value("serviceName", "")))
	case "tcp":
		if header, single := options.one("headerType"); (!single && len(options["headerType"]) > 0) || single && header != "none" {
			yamlx.Set(params, "network", yamlx.Str("tcp"))
			yamlx.Set(params, "tcp-opts", yamlx.Map(yamlx.Str("header"), yamlx.Map(yamlx.Str("type"), options.value("headerType", "none"))))
		}
	}
	if len(options["flow"]) > 0 {
		yamlx.Set(params, "flow", options.value("flow", ""))
	}
	if values := options["alpn"]; len(values) > 0 {
		if len(values) != 1 {
			return nil, errShareLink
		}
		parts := strings.Split(values[0], ",")
		items := make([]*yamlx.Node, 0, len(parts))
		for _, part := range parts {
			items = append(items, yamlx.Str(part))
		}
		yamlx.Set(params, "alpn", yamlx.Seq(items...))
	}
	return makeNode(name, "vless", serverPort[:at], port, params)
}
func parseTrojanNode(uri string) (*model.Node, error) {
	uri, name := fragment(strings.TrimPrefix(uri, "trojan://"), "Trojan Node")
	password, serverInfo, err := splitPair(uri, "@")
	if err != nil {
		return nil, err
	}
	server, portText, err := splitPair(serverInfo, ":")
	if err != nil {
		return nil, err
	}
	options := queryOptions{}
	if port, query, ok := strings.Cut(portText, "?"); ok {
		portText = port
		options = queryValues(query)
	}
	port, err := parseURIPort(portText)
	if err != nil {
		return nil, err
	}
	insecure, _ := options.one("allowInsecure")
	return makeNode(name, "trojan", server, port, yamlx.Map(yamlx.Str("password"), yamlx.Str(password), yamlx.Str("sni"), options.value("sni", ""), yamlx.Str("skip-cert-verify"), yamlx.Bool(insecure == "true")))
}
func parseHysteriaNode(uri string) (*model.Node, error) {
	kind := "hysteria"
	name := "Hysteria Node"
	if strings.HasPrefix(uri, "hysteria2://") {
		kind = "hysteria2"
		name = "Hysteria2 Node"
	}
	uri, name = fragment(strings.TrimPrefix(uri, kind+"://"), name)
	auth, serverInfo := "", uri
	if user, server, ok := strings.Cut(uri, "@"); ok {
		auth = user
		serverInfo = server
	}
	serverPort := serverInfo
	options := queryOptions{}
	if server, query, ok := strings.Cut(serverInfo, "?"); ok {
		serverPort = server
		options = queryValues(query)
	}
	first := func(key string) string {
		if values := options[key]; len(values) > 0 {
			return values[0]
		}
		return ""
	}
	server, portText, err := splitPair(serverPort, ":")
	if err != nil {
		return nil, err
	}
	port, err := parseURIPort(portText)
	if err != nil {
		return nil, err
	}
	sni := first("peer")
	if sni == "" {
		sni = first("sni")
	}
	return makeNode(name, kind, server, port, yamlx.Map(yamlx.Str("password"), yamlx.Str(auth), yamlx.Str("obfs"), yamlx.Str(first("obfs")), yamlx.Str("sni"), yamlx.Str(sni), yamlx.Str("skip-cert-verify"), yamlx.Bool(first("insecure") == "1")))
}
