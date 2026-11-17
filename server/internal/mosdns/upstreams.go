package mosdns

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// ParseDNSUpstreams is parse_dns_upstreams(dns_config): a YAML list of
// upstream mappings (or strings), a single YAML mapping with addr, or the
// simple format, one upstream per line:
//
//	https://1.1.1.1/dns-query bootstrap=223.5.5.5
//	tls://8.8.8.8 223.5.5.5
//	223.5.5.5
//
// JSON-compatible upstreams retain the existing configuration interface;
// YAML-only mappings remain Nodes until the generator writes the document.
func ParseDNSUpstreams(dnsConfig any) (out []any, err error) {
	defer catch(&err)
	out, err = parseDNSUpstreams(dnsConfig)
	if err != nil {
		return nil, err
	}
	for i, value := range out {
		if node, ok := value.(*yamlx.Node); ok {
			data, decodeErr := yamlx.ToJSON(node)
			if decodeErr != nil {
				if hasNonStringKey(node) {
					continue
				}
				return nil, decodeErr
			}
			value, decodeErr := jsonx.Parse(data)
			if decodeErr == nil {
				out[i] = value
			}
			// A native graph may exceed the legacy JSON parser's nesting
			// representation. Retain that graph instead of losing its data.

		}
	}
	return out, nil
}

func parseDNSUpstreams(dnsConfig any) ([]any, error) {
	if !truthy(dnsConfig) || strip(dnsConfig) == "" {
		return []any{}, nil
	}
	text := pystr.Strip(dnsConfig.(string))

	upstreams := []any{}
	parsed, parseErr := yamlx.Parse([]byte(text))
	if parseErr != nil && rejectYAML(parseErr) {
		return nil, customYAMLError(parseErr)
	}
	if parseErr == nil {
		parsed = yamlx.Resolve(parsed)
		switch parsed.Kind {
		case yaml.SequenceNode:
			for _, entry := range parsed.Content {
				node := yamlx.Resolve(entry)
				if node.Kind == yaml.MappingNode && yamlx.Get(node, "addr") != nil {
					upstreams = append(upstreams, entry)
				}
				if textScalar(node) {
					upstreams = append(upstreams, jsonx.Obj("addr", node.Value))
				}
			}
			if len(upstreams) > 0 {
				return upstreams, nil
			}
		case yaml.MappingNode:
			if yamlx.Get(parsed, "addr") != nil {
				return []any{parsed}, nil
			}
		}
	}

	for _, line := range strings.Split(text, "\n") {
		if line != "" && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		line = pystr.Strip(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.FieldsFunc(line, pystr.IsSpace)
		if len(parts) == 0 {
			continue
		}
		upstream := jsonx.NewObject()
		upstream.Set("addr", parts[0])
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if key, value, ok := strings.Cut(part, "="); ok {
				switch key {
				case "bootstrap":
					upstream.Set("bootstrap", value)
				case "enable_pipeline":
					switch pystr.Lower(value) {
					case "true", "1", "yes":
						upstream.Set("enable_pipeline", true)
					case "false", "0", "no":
						upstream.Set("enable_pipeline", false)
					}
				}
			} else if i == 1 {
				upstream.Set("bootstrap", part)
			}
		}
		upstreams = append(upstreams, upstream)
	}
	return upstreams, nil
}
