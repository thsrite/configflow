package deploy

import (
	"bytes"
	"configflow/internal/agents/diag"
	"configflow/internal/yamlx"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const geoBase = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"

type geoResource struct {
	key, label, path, defaultFile string
	aliases                       []string
}

var geoResources = []geoResource{{"geosite", "GeoSite", "GeoSite.dat", "geosite.dat", []string{"geosite.dat"}}, {"geoip", "GeoIP", "GeoIP.dat", "geoip.dat", []string{"geoip.dat"}}, {"mmdb", "GeoIP", "geoip.metadb", "geoip.metadb", []string{"country.mmdb", "geoip.db", "geoip.metadb"}}, {"asn", "ASN", "ASN.mmdb", "GeoLite2-ASN.mmdb", []string{"asn.mmdb"}}}
var geositeRule = regexp.MustCompile(`(?i)(?:^|[,(])\s*GEOSITE\s*,`)
var geoipRule = regexp.MustCompile(`(?i)(?:^|[,(])\s*(?:SRC-)?GEOIP\s*,\s*([^,()]+)`)
var asnRule = regexp.MustCompile(`(?i)(?:^|[,(])\s*(?:SRC-)?IP-ASN\s*,`)

// RequiredGeoResources inspects all Mihomo consumers, including disabled DNS
// and sniffer sections (the core still parses their filters).
func RequiredGeoResources(doc *yamlx.Node, files map[string]Content) []string {
	required := map[string]bool{}
	geoip := func(code string) {
		if !strings.EqualFold(strings.TrimSpace(code), "lan") {
			required["geoip"] = true
		}
	}
	visited := map[*yamlx.Node]bool{}
	var rules func(*yamlx.Node)
	rules = func(n *yamlx.Node) {
		n = yamlx.Resolve(n)
		if n == nil || visited[n] {
			return
		}
		visited[n] = true
		if n.Kind == 2 || n.Kind == 4 {
			for _, child := range n.Content {
				rules(child)
			}
			return
		}
		if n.Tag != "!!str" {
			return
		}
		if geositeRule.MatchString(n.Value) {
			required["geosite"] = true
		}
		for _, m := range geoipRule.FindAllStringSubmatch(n.Value, -1) {
			geoip(m[1])
		}
		if asnRule.MatchString(n.Value) {
			required["asn"] = true
		}
	}
	list := func(n *yamlx.Node, fn func(string)) {
		n = yamlx.Resolve(n)
		if n != nil && n.Kind == 2 {
			for _, v := range n.Content {
				fn(scalar(v))
			}
		}
	}
	geosite := func(v string) {
		if strings.HasPrefix(strings.ToLower(v), "geosite:") {
			required["geosite"] = true
		}
	}
	rules(yamlx.Get(doc, "rules"))
	rules(yamlx.Get(doc, "sub-rules"))
	for _, p := range yamlx.Pairs(yamlx.Get(doc, "rule-providers")) {
		if field(p, "behavior") != "classical" {
			continue
		}
		rules(yamlx.Get(p, "payload"))
		entry, ok := files[strings.TrimPrefix(field(p, "path"), "./")]
		format := field(p, "format")
		if format == "" {
			format = "yaml"
		}
		if !ok || !utf8.Valid(entry.Data) {
			continue
		}
		if format == "yaml" {
			if n, err := yamlx.Parse(bytes.TrimPrefix(entry.Data, []byte{0xef, 0xbb, 0xbf})); err == nil {
				rules(n)
			}
		} else if format == "text" {
			for _, line := range strings.Split(string(entry.Data), "\n") {
				rules(yamlx.Str(line))
			}
		}
	}
	dns := yamlx.Get(doc, "dns")
	for _, key := range []string{"nameserver-policy", "proxy-server-nameserver-policy"} {
		for name := range yamlx.Pairs(yamlx.Get(dns, key)) {
			geosite(name)
		}
	}
	if field(dns, "enhanced-mode") == "fake-ip" {
		filters := yamlx.Get(dns, "fake-ip-filter")
		if field(dns, "fake-ip-filter-mode") == "rule" {
			rules(filters)
		} else {
			list(filters, geosite)
		}
	}
	if truth(yamlx.Get(dns, "fallback")) {
		filter := yamlx.Get(dns, "fallback-filter")
		if v := yamlx.Get(filter, "geoip"); v == nil || truth(v) {
			code := field(filter, "geoip-code")
			if code == "" {
				code = "CN"
			}
			geoip(code)
		}
		if truth(yamlx.Get(filter, "geosite")) {
			required["geosite"] = true
		}
	}
	sniffer := yamlx.Get(doc, "sniffer")
	for _, key := range []string{"force-domain", "skip-domain"} {
		list(yamlx.Get(sniffer, key), geosite)
	}
	for _, key := range []string{"skip-src-address", "skip-dst-address"} {
		list(yamlx.Get(sniffer, key), func(v string) {
			if strings.HasPrefix(strings.ToLower(v), "geoip:") {
				for _, code := range strings.Split(strings.SplitN(v, ":", 2)[1], ",") {
					geoip(code)
				}
			}
		})
	}
	mode := yamlx.Get(doc, "geodata-mode")
	if required["geoip"] && (mode == nil || mode.Tag != "!!bool" || mode.Value != "true") {
		delete(required, "geoip")
		required["mmdb"] = true
	}
	out := []string{}
	for _, r := range geoResources {
		if required[r.key] {
			out = append(out, r.key)
		}
	}
	return out
}

// PrepareMihomoResources reuses Agent assets and adds only missing databases.
// Geo files are supplemental and never enter content Fingerprint.
func PrepareMihomoResources(ctx context.Context, b *Bundle, c Capabilities, src *Source, p Progress) (*Bundle, error) {
	if b.manifest.ServiceType != "mihomo" || c.MihomoResources == nil {
		return b, nil
	}
	files, err := b.Contents()
	if err != nil {
		return nil, err
	}
	doc, err := yamlx.Parse(files[b.manifest.ConfigPath].Data)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for _, name := range c.MihomoResources {
		existing[strings.ToLower(name)] = true
	}
	for name := range files {
		existing[strings.ToLower(name)] = true
	}
	sources := yamlx.Resolve(yamlx.Get(doc, "geox-url"))
	if truth(sources) && sources.Kind != 4 {
		return nil, preparation(msgGeoxMapping)
	}
	m := b.Manifest()
	required := RequiredGeoResources(doc, files)
	phase := func(message string) {
		if p.Phase != nil {
			p.Phase(message)
		}
	}
	for _, key := range required {
		var resource geoResource
		for _, r := range geoResources {
			if r.key == key {
				resource = r
				break
			}
		}
		reuse := false
		for _, alias := range resource.aliases {
			reuse = reuse || existing[alias]
		}
		if reuse {
			phase(fmt.Sprintf(msgGeoReuse, resource.label, resource.path))
			continue
		}
		rawURL := geoBase + resource.defaultFile
		v := yamlx.Get(sources, key)
		if v != nil {
			rawURL = scalar(v)
		}
		dir := c.ConfigDirectory
		if dir == "" {
			dir = msgAgentConfigDir
		}
		target := path.Join(dir, resource.path)
		var data []byte
		var failure error
		if v != nil && (v.Tag != "!!str" || strings.TrimSpace(rawURL) == "") {
			failure = &RuleDownloadError{&PreparationError{Message: fmt.Sprintf(msgGeoInvalidURL, key)}}
		} else {
			phase(fmt.Sprintf(msgGeoDownload, resource.label, resource.path))
			data, failure = fetchBytes(ctx, rawURL, src)
			if failure == nil {
				phase(fmt.Sprintf(msgGeoCheck, resource.path))
				trimmed := bytes.ToLower(bytes.TrimSpace(data))
				if len(data) == 0 || bytes.HasPrefix(trimmed, []byte("<!doctype html")) || bytes.HasPrefix(trimmed, []byte("<html")) {
					failure = &RuleDownloadError{&PreparationError{Message: msgGeoInvalid}}
				}
			}
		}
		if failure != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var download *RuleDownloadError
			if !errors.As(failure, &download) {
				return nil, failure
			}
			reason, details, _ := strings.Cut(failure.Error(), "\n")
			message := fmt.Sprintf(msgGeoFailure, resource.label, reason, rawURL, target, key, target)
			if details != "" {
				message += "\n" + details
			}
			return nil, &RuleDownloadError{&PreparationError{Message: diag.FailureText(message, nil, 0), Remedy: download.Remedy, cause: failure}}
		}
		total := int64(len(data))
		for _, file := range files {
			total += int64(len(file.Data))
		}
		if len(data) > MaxFileBytes || len(files) >= MaxFiles || total > MaxTotalBytes {
			return nil, preparation(fmt.Sprintf(msgGeoLimit, resource.label))
		}
		files[resource.path] = Content{data, RoleResource}
		m.Files = append(m.Files, File{Path: resource.path, Name: resource.label, Role: RoleResource, Size: int64(len(data)), SHA256: digest(data)})
		phase(fmt.Sprintf(msgGeoReady, resource.path, commaNumber(len(data))))
	}
	if len(files) == len(b.manifest.Files) {
		return b, nil
	}
	return Pack(m, files, b.ConfigVersion(), b.Comparison())
}
func commaNumber(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
