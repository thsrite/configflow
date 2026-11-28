package deploy

import (
	"bytes"
	"configflow/internal/agents/diag"
	"configflow/internal/gateway"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/yamlx"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func scalar(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n == nil {
		return ""
	}
	return n.Value
}
func field(n *yamlx.Node, key string) string { return scalar(yamlx.Get(n, key)) }

// truth is the truthiness of a YAML value: empty collections, null, false,
// numeric zero (any notation, e.g. 0x0 or 0.0) and the empty string are false.
func truth(n *yamlx.Node) bool {
	n = yamlx.Resolve(n)
	if n == nil {
		return false
	}
	if n.Kind == yaml.SequenceNode || n.Kind == yaml.MappingNode {
		return len(n.Content) > 0
	}
	var v any
	if err := yamlx.Decode(n, &v); err != nil {
		return n.Value != ""
	}
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case uint64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// isTrue reports a YAML boolean true (Python "is True": not 1, not "true").
func isTrue(n *yamlx.Node) bool {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.ScalarNode {
		return false
	}
	var v any
	return yamlx.Decode(n, &v) == nil && v == true
}
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate deployment ID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// RenderConfig serializes a generated mapping with the optional local DNS probe.
func RenderConfig(main *yamlx.Node, service string, localDNSWatchdog bool) ([]byte, error) {
	if localDNSWatchdog && (service == "mosdns" || service == "oxidns") {
		var err error
		main, err = oxidns.EnsureLocalDNSWatchdog(main)
		if err != nil {
			return nil, PreparationContext(err, service, "config.yaml", msgConfig)
		}
	}
	return yamlx.Marshal(main)
}

type bundleBuilder struct {
	service              string
	files                map[string]Content
	names                map[string]string
	ruleTypes, ruleNames map[string]string
	total                int64
}

func (b *bundleBuilder) add(path string, value []byte, role Role, native, mixed bool, name string, empty []byte) (string, error) {
	kind := msgAttachment
	switch role {
	case RoleProvider:
		kind = msgSubscription
	case RoleConfig:
		kind = msgConfig
	case RoleRule:
		kind = msgRules
	}
	if name == "" {
		name = b.ruleNames[strings.TrimPrefix(path, "./")]
	}
	target, err := b.addContent(path, value, role, native, mixed, empty)
	if err != nil {
		return "", PreparationContext(err, name, path, kind)
	}
	if name != "" {
		b.names[target] = diag.FailureText(name, nil, 512)
	}
	return target, nil
}
func (b *bundleBuilder) addContent(path string, value []byte, role Role, native, mixed bool, empty []byte) (string, error) {
	p, err := RelativePath(path)
	if err != nil {
		return "", err
	}
	if b.service == "oxidns" && (p == "state" || strings.HasPrefix(p, "state/")) {
		return "", preparation("OxiDNS runtime state cannot be managed by a deployment")
	}
	// nil is a valid explicitly empty []byte as well as an omitted typed value;
	// all BuildInput content fields are bytes, so both mean an empty file.
	if kind, ok := b.ruleTypes[p]; ok && !(b.service == "oxidns" && native) {
		value, err = ConvertMosDNSRules(value, kind, mixed)
		if err != nil {
			return "", err
		}
		if len(value) == 0 {
			value = empty
		}
	}
	if len(value) > MaxFileBytes {
		return "", preparation("Deployment file exceeds size limit")
	}
	if _, ok := b.files[p]; ok {
		return "", preparation("Duplicate deployment path: " + p)
	}
	if len(b.files) >= MaxFiles || b.total+int64(len(value)) > MaxTotalBytes {
		return "", preparation("Deployment exceeds size limit")
	}
	b.files[p] = Content{bytes.Clone(value), role}
	b.total += int64(len(value))
	return p, nil
}

type ruleDownload struct {
	rule RuleRef
	path string
}
type downloadResult struct {
	item ruleDownload
	data []byte
	err  error
}

// Build captures the complete managed graph before any Agent upload. All worker
// goroutines are joined before return, including when one source fails.
func Build(ctx context.Context, in BuildInput) (*Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service := in.Target.ServiceType
	if service == "" {
		service = "mihomo"
	}
	if service != "mihomo" && service != "mosdns" && service != "oxidns" {
		return nil, preparation("Unsupported transactional deployment service")
	}
	main, err := yamlx.Parse([]byte(in.ConfigContent))
	if err != nil {
		return nil, PreparationContext(SourceError(msgInvalidGeneratedYAML, in.ConfigContent, Position{}, err, ""), service, "config.yaml", msgConfig)
	}
	if main.Kind != yaml.MappingNode {
		return nil, preparation("Generated configuration must be a YAML mapping")
	}
	if service == "oxidns" && truth(yamlx.Get(main, "include")) {
		return nil, preparation("OxiDNS deployment must inline included plugin definitions")
	}
	b := &bundleBuilder{service: service, files: map[string]Content{}, names: map[string]string{}, ruleTypes: map[string]string{}, ruleNames: map[string]string{}}
	plugins := yamlx.Resolve(yamlx.Get(main, "plugins"))
	if service == "mosdns" || service == "oxidns" {
		if plugins != nil {
			for _, plugin := range plugins.Content {
				kind := field(plugin, "type")
				if kind != "domain_set" && kind != "ip_set" {
					continue
				}
				args := yamlx.Resolve(yamlx.Get(plugin, "args"))
				if !truth(args) {
					continue
				}
				refs := yamlx.Resolve(yamlx.Get(args, "files"))
				if args.Kind != yaml.MappingNode || (refs != nil && refs.Kind != yaml.SequenceNode) {
					return nil, preparation("MosDNS file references must be a list")
				}
				if refs == nil {
					continue
				}
				for _, ref := range refs.Content {
					if service == "oxidns" {
						_, relative, err := oxidns.RelativeResource(ref)
						if err != nil {
							return nil, err
						}
						if !relative {
							continue
						}
					}
					p, err := RelativePath(scalar(ref))
					if err != nil {
						return nil, err
					}
					if old, ok := b.ruleTypes[p]; ok && old != kind {
						return nil, preparation("MosDNS file is used by both domain_set and ip_set: " + p)
					}
					b.ruleTypes[p], b.ruleNames[p] = kind, field(plugin, "tag")
				}
			}
		}
	}
	for _, f := range in.ProviderDownloads {
		if _, err := b.add(f.LocalPath, f.Content, RoleProvider, false, false, f.Name, nil); err != nil {
			return nil, err
		}
	}
	for _, f := range in.CustomFiles {
		if _, err := b.add(f.Path, f.Content, RoleCustom, f.Native, false, f.Name, nil); err != nil {
			return nil, err
		}
	}
	pinned := map[string]bool{}
	for _, f := range in.PinnedFiles {
		if f.Role != RoleRule && f.Role != RoleCustom && f.Role != RoleProvider {
			return nil, preparation("Invalid pinned deployment file role")
		}
		p, err := b.add(f.Path, f.Content, f.Role, true, false, "", nil)
		if err != nil {
			return nil, err
		}
		pinned[p] = true
	}
	if service == "mihomo" && in.Source.gatewayEnabled() {
		for _, f := range sortedContents(b.files) {
			v := b.files[f]
			if v.Role != RoleProvider {
				continue
			}
			if !utf8.Valid(v.Data) {
				return nil, preparation(msgGatewayProviderInvalid)
			}
			provider, err := yamlx.Parse(v.Data)
			if err != nil {
				return nil, &PreparationError{Message: msgGatewayProviderInvalid, cause: err}
			}
			// yamlx checks alias work before expansion; gateway traverses each shared
			// container once and handles cycles. Never convert hostile providers to jsonx.
			if gateway.RoutingMarkPath(provider, "") != "" {
				return nil, preparation(msgGatewayRoutingMark)
			}
		}
	}
	downloads := []ruleDownload{}
	for _, d := range in.RulesetDownloads {
		r, p, err := downloadRef(d)
		if err != nil {
			return nil, err
		}
		downloads = append(downloads, ruleDownload{r, p})
	}
	if service == "mihomo" {
		if providers := yamlx.Get(main, "rule-providers"); providers != nil {
			if providers.Kind != yaml.MappingNode {
				return nil, preparation("Rule providers must be a mapping")
			}
			downloads = nil
			for name, definition := range yamlx.Pairs(providers) {
				if field(definition, "type") == "inline" {
					yamlx.Delete(definition, "path")
					continue
				}
				p := field(definition, "path")
				if p == "" {
					return nil, preparation(fmt.Sprintf("Rule provider %s has no bundled path", name))
				}
				p, err = RelativePath(p)
				if err != nil {
					return nil, err
				}
				if _, ok := b.files[p]; ok {
					continue
				}
				u := field(definition, "url")
				if u == "" {
					return nil, preparation(fmt.Sprintf("Rule provider %s references an unbundled local file; use a managed rule source", name))
				}
				format := field(definition, "format")
				if format == "" {
					format = "yaml"
				}
				downloads = append(downloads, ruleDownload{RuleRef{Name: name, URL: u, Behavior: field(definition, "behavior"), Format: format}, p})
			}
		}
	}
	filtered := downloads[:0]
	for _, d := range downloads {
		p, err := RelativePath(d.path)
		if err != nil {
			return nil, err
		}
		if !pinned[p] {
			filtered = append(filtered, d)
		}
	}
	downloads = filtered
	cache := newRawCache()
	fetch := func(ctx context.Context, u string) ([]byte, error) {
		return cache.get(ctx, u, func(ctx context.Context, u string) ([]byte, error) { return fetchBytes(ctx, u, in.Source) })
	}
	results := make(chan downloadResult, len(downloads))
	jobs := make(chan ruleDownload, len(downloads))
	for _, d := range downloads {
		jobs <- d
	}
	close(jobs)
	if in.Progress.Rules != nil {
		in.Progress.Rules(0, len(downloads))
	}
	var wg sync.WaitGroup
	for range min(downloadWorkers, len(downloads)) {
		wg.Go(func() {
			for item := range jobs {
				func() {
					result := downloadResult{item: item}
					defer func() {
						if recovered := recover(); recovered != nil {
							result.err = fmt.Errorf("prepare rule panic: %v", recovered)
							logger.Exception("prepare rule panic", result.err)
						}
						results <- result
					}()
					result.data, result.err = downloadRule(ctx, in, service, b.ruleTypes, item, fetch, len(downloads))
				}()
			}
		})
	}
	// The collector mutates files and reports completion on the caller goroutine.
	var firstErr error
	prepared := 0
	for done := 1; done <= len(downloads); done++ {
		result := <-results
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		item := result.item
		if firstErr == nil {
			_, err = b.add(item.path, result.data, RoleRule, false, item.rule.Behavior == "classical", item.rule.Name, emptyRule(item.rule.URL))
			if err != nil {
				firstErr = err
			}
		}
		if in.Progress.Detail != nil {
			prepared++
			n := prepared
			in.Progress.Detail(&n, len(downloads), displayName(item.rule.Name), "prepared")
		} else if in.Progress.Rules != nil {
			prepared++
			in.Progress.Rules(prepared, len(downloads))
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if service == "mihomo" {
		for _, section := range []string{"proxy-providers", "rule-providers"} {
			for name, provider := range yamlx.Pairs(yamlx.Get(main, section)) {
				reference := yamlx.Get(provider, "path")
				if reference == nil {
					if field(provider, "type") == "inline" {
						continue
					}
					return nil, preparation(fmt.Sprintf("Provider %s has no bundled path", name))
				}
				p, err := RelativePath(scalar(reference))
				if err != nil {
					return nil, err
				}
				if _, ok := b.files[p]; !ok {
					return nil, preparation(fmt.Sprintf("Provider %s has no bundled file", name))
				}
				yamlx.Set(provider, "path", yamlx.Str("./"+p))
			}
		}
	} else if service == "oxidns" {
		paths := sortedContents(b.files)
		if err := oxidns.ValidateResourceFiles(main, paths); err != nil {
			refs, refErr := oxidns.ResourceReferences(main)
			if refErr != nil {
				return nil, refErr
			}
			missingSet := map[string]bool{}
			for _, ref := range refs {
				p, relative, e := oxidns.RelativeResource(yamlx.Str(ref))
				if e != nil {
					return nil, e
				}
				if relative {
					if _, ok := b.files[p]; !ok {
						missingSet[p] = true
					}
				}
			}
			missing := make([]string, 0, len(missingSet))
			for p := range missingSet {
				missing = append(missing, p)
			}
			sort.Strings(missing)
			if hints := in.Source.unavailable(missing); len(hints) > 0 {
				return nil, preparation(msgOxidnsUnavailable + "\n· " + strings.Join(hints, "\n· "))
			}
			if len(missing) > 0 {
				return nil, preparation(fmt.Sprintf(msgOxidnsMissing, missing[0]))
			}
			return nil, &PreparationError{Message: err.Error(), cause: err}
		}
	} else if plugins != nil {
		for _, plugin := range plugins.Content {
			args := yamlx.Resolve(yamlx.Get(plugin, "args"))
			if args == nil || args.Kind != yaml.MappingNode {
				continue
			}
			refs := yamlx.Resolve(yamlx.Get(args, "files"))
			if refs == nil {
				continue
			}
			if refs.Kind != yaml.SequenceNode {
				return nil, preparation("MosDNS file references must be a list")
			}
			for i, ref := range refs.Content {
				p, err := RelativePath(scalar(ref))
				if err != nil {
					return nil, err
				}
				if _, ok := b.files[p]; !ok {
					return nil, preparation("MosDNS references an unbundled file: " + p)
				}
				refs.Content[i] = yamlx.Str("./" + p)
			}
		}
	}
	content, err := RenderConfig(main, service, in.LocalDNSWatchdog)
	if err != nil {
		return nil, err
	}
	if _, err = b.add("config.yaml", content, RoleConfig, false, false, "", nil); err != nil {
		return nil, err
	}
	id := in.DeploymentID
	if id == "" {
		id, err = randomID()
		if err != nil {
			return nil, err
		}
	}
	profile, err := resolvedBuildProfile(ctx, in.repository(), in.Source, in.Target)
	if err != nil {
		return nil, err
	}
	m := Manifest{ProtocolVersion: 1, DeploymentID: id, AgentID: in.Target.ID, ProfileID: profile, ServiceType: service, ConfigPath: "config.yaml", Files: []File{}}
	for _, p := range sortedContents(b.files) {
		c := b.files[p]
		m.Files = append(m.Files, File{Path: p, Size: int64(len(c.Data)), SHA256: digest(c.Data), Role: c.Role, Name: b.names[p]})
	}
	if in.Progress.Phase != nil {
		in.Progress.Phase(fmt.Sprintf(msgPacking, len(b.files)))
	}
	return Pack(m, b.files, digest(content)[:8], nil)
}
func sortedContents(files map[string]Content) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
func displayName(name string) string {
	if name == "" {
		return msgUnnamed
	}
	return name
}
func emptyRule(rawURL string) []byte {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	if !strings.HasSuffix(u.Path, "/mosdns/rule-proxy") && !strings.HasSuffix(u.Path, "/oxidns/rule-proxy") {
		return nil
	}
	part := u.Query().Get("part")
	if part == "domain" || part == "ip" {
		return []byte(mosdns.EmptyRulePart(part))
	}
	return nil
}
func downloadRule(ctx context.Context, in BuildInput, service string, ruleTypes map[string]string, item ruleDownload, fetch func(context.Context, string) ([]byte, error), total int) ([]byte, error) {
	if in.Progress.Detail != nil {
		in.Progress.Detail(nil, total, displayName(item.rule.Name), "preparing")
	}
	if in.Progress.Message != nil {
		ctx = rulefetch.WithRetryObserver(ctx, func(attempt int) {
			in.Progress.Message(fmt.Sprintf("正在重试下载规则「%s」（第 %d 次）", displayName(item.rule.Name), attempt))
		})
	}
	p, err := RelativePath(item.path)
	if err != nil {
		return nil, err
	}
	remote := map[string]bool{}
	o := MaterializeOptions{FetchRaw: fetch, RawMosDNS: ruleTypes[p] != "", RemoteSources: remote, Refresh: in.Refresh}
	if item.rule.Behavior != "" {
		o.DeclaredRule = &item.rule
	}
	value, err := MaterializeRule(ctx, item.rule.URL, in.Source, in.BaseURL, o)
	if err == nil && service == "mihomo" {
		u, _ := url.Parse(item.rule.URL)
		if u != nil && u.Query().Get("target") == "mihomo" {
			rule, e := LocalRule(item.rule.URL, in.Source, in.BaseURL, true)
			if e != nil {
				err = e
			} else if rule != nil && defaultBehavior(rule.Behavior) == defaultBehavior(item.rule.Behavior) && utf8.Valid(value) {
				value, err = in.Source.normalize(value, *rule)
			}
		}
		if err == nil && len(remote) > 0 && item.rule.Behavior != "" {
			err = in.Source.validateRemote(value, item.rule)
		}
	}
	if err == nil {
		return value, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var download *RuleDownloadError
	if errors.As(err, &download) {
		reason, details, _ := strings.Cut(err.Error(), "\n")
		message := fmt.Sprintf(msgRuleDownload, displayName(item.rule.Name), reason, diag.FailureText(item.path, nil, 0))
		if details != "" {
			message += "\n" + details
		}
		return nil, &RuleDownloadError{&PreparationError{Message: message, Remedy: download.Remedy, cause: err}}
	}
	var prep *PreparationError
	var remoteErr *rulefetch.RemoteRuleValidationError
	if errors.As(err, &prep) || errors.As(err, &remoteErr) {
		return nil, PreparationContext(err, item.rule.Name, item.path, msgRules)
	}
	return nil, err
}
func defaultBehavior(s string) string {
	if s == "" {
		return "domain"
	}
	return s
}

// repository is the repository that resolves the default profile of a target
// without one: in.Repo, else the refresh Service's repository.
func (in BuildInput) repository() *repo.Repository {
	if in.Repo != nil {
		return in.Repo
	}
	return in.Refresh.Repository()
}
