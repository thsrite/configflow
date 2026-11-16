package mosdns

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
)

// ParseDNSUpstreams is parse_dns_upstreams(dns_config): a YAML list of
// upstream mappings (or strings), a single YAML mapping with addr, or the
// simple format, one upstream per line:
//
//	https://1.1.1.1/dns-query bootstrap=223.5.5.5
//	tls://8.8.8.8 223.5.5.5
//	223.5.5.5
//
// The items are the loaded YAML values themselves (*jsonx.Object, or
// *yamlx.Mapping for a mapping with non-string keys). The error is Python's
// AttributeError for a truthy value that is not a string.
func ParseDNSUpstreams(dnsConfig any) (out []any, err error) {
	defer catch(&err)
	return parseDNSUpstreams(dnsConfig), nil
}

func parseDNSUpstreams(dnsConfig any) []any {
	if !truthy(dnsConfig) || strip(dnsConfig) == "" {
		return []any{}
	}
	text := pystr.Strip(dnsConfig.(string))

	upstreams := []any{}
	// Every exception inside Python's try falls back to the line format.
	if parsed, err := yamlx.LoadWith(text, yamlx.LoadOptions{KeepNonStringKeys: true}); err == nil {
		switch x := parsed.(type) {
		case []any:
			for _, entry := range x {
				if yamlx.IsMapping(entry) {
					if _, ok := yamlx.Lookup(entry, "addr"); ok {
						upstreams = append(upstreams, entry)
					}
				} else if s, ok := entry.(string); ok {
					upstreams = append(upstreams, jsonx.Obj("addr", s))
				}
			}
			if len(upstreams) > 0 {
				return upstreams
			}
		case *jsonx.Object, *yamlx.Mapping:
			if _, ok := yamlx.Lookup(x, "addr"); ok {
				return []any{x}
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
	return upstreams
}
