package mosdns

// MosDNS ZIP archive (backend/utils/mosdns_archive.py): every rule file is
// materialized from the captured profile snapshot without HTTP calls back
// to ConfigFlow; each remote source is fetched once, at most MaxWorkers at
// a time.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rulesetfmt"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

var archiveLogger = logx.Get("backend.utils.mosdns_archive")

// MaxWorkers is the number of concurrent source downloads of one archive.
const MaxWorkers = 8

// Archive limits (variables so tests can lower them).
var (
	MaxFiles      = 4096
	MaxTotalBytes = 256 * 1024 * 1024
)

// ArchiveError is a safe, user-facing archive failure (MosdnsArchiveError,
// also used for OxiDNS archives): it never contains a source URL or
// credentials.
type ArchiveError struct{ Message string }

func (e *ArchiveError) Error() string { return e.Message }

func archiveErr(message string) error { return &ArchiveError{Message: message} }

// Download is one rule file to materialize (an entry of
// GetMosDNSRulesetDownloads).
type Download struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Behavior  string `json:"behavior"`
}

// File is a file stored as is (GetMosDNSCustomFiles, OxiDNS attachments).
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ArchiveOptions are the options of BuildMosDNSZip.
type ArchiveOptions struct {
	BaseURL string
	// SkipReferenceValidation skips the check that every `files` entry of
	// the config is packed (the OxiDNS archive).
	SkipReferenceValidation bool
	Repo                    *repo.Repository
}

// ArchivePath validates a path inside the archive: relative, no empty,
// "." or ".." parts, no backslash or control character ("./" prefixes are
// dropped).
func ArchivePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return r < 32 }) >= 0 {
		return "", archiveErr("规则文件路径无效")
	}
	for strings.HasPrefix(value, "./") {
		value = value[2:]
	}
	if strings.HasPrefix(value, "/") {
		return "", archiveErr("规则文件必须使用安全的相对路径")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", archiveErr("规则文件必须使用安全的相对路径")
		}
	}
	return value, nil
}

// label is a rule name safe for errors and logs.
func label(name string) string {
	if name == "" {
		name = "未命名规则"
	}
	lower := strings.ToLower(name)
	if strings.Contains(name, "://") || strings.Contains(lower, "token=") || strings.Contains(lower, "password=") ||
		strings.Contains(lower, "secret=") {
		return "未命名规则"
	}
	var b strings.Builder
	count := 0
	for _, r := range name {
		if r >= 32 && r != 127 && count < 160 {
			b.WriteRune(r)
			count++
		}
	}
	return b.String()
}

// snapshot contains only the typed metadata needed by archive callbacks.
type snapshot struct {
	ProfileID    string
	SystemConfig model.SystemConfig
	RuleConfigs  []model.RuleConfig
	RuleLibrary  []model.RuleLibraryItem
}

// Security decisions require literal stored strings and booleans rather than
// R5's display-text coercion. Malformed grants fail closed.
func snapshotOf(config *model.Config) snapshot {
	result := snapshot{ProfileID: config.ProfileID, SystemConfig: config.SystemConfig}
	system := storedMembers(config.SystemConfig)
	result.SystemConfig.ConfigToken = credentialMember(system, "config_token")
	result.SystemConfig.RuleProxyToken = credentialMember(system, "rule_proxy_token")
	for _, rule := range config.Profile.RuleConfigs {
		members := storedMembers(rule)
		rule.LibraryRuleID = credentialMember(members, "library_rule_id")
		allowed := literalFlag(members, "enabled") && literalFlag(members, "library_enabled")
		rule.Enabled = &allowed
		rule.LibraryEnabled = &allowed
		result.RuleConfigs = append(result.RuleConfigs, rule)
	}
	for _, item := range config.RuleLibrary {
		members := storedMembers(item)
		item.ID = credentialMember(members, "id")
		item.Name = credentialMember(members, "name")
		content := credentialMember(members, "content")
		item.Content = &content
		result.RuleLibrary = append(result.RuleLibrary, item)
	}
	return result
}
func storedMembers(value any) model.Fields {
	data, err := model.Marshal(value)
	if err != nil {
		return model.Fields{}
	}
	var fields model.Fields
	if json.Unmarshal(data, &fields) != nil {
		return model.Fields{}
	}
	return fields
}
func credentialMember(fields model.Fields, key string) string {
	data, ok := fields.Get(key)
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(data, &value) != nil {
		return ""
	}
	return value
}
func literalFlag(fields model.Fields, key string) bool {
	data, ok := fields.Get(key)
	if !ok {
		return true
	}
	var value bool
	return json.Unmarshal(data, &value) == nil && value
}

func (s *snapshot) selected(id string) bool {
	if id == "" {
		return false
	}
	for _, rule := range s.RuleConfigs {
		if rule.LibraryRuleID == id && flag(rule.Enabled) && flag(rule.LibraryEnabled) {
			return true
		}
	}
	return false
}

type origin struct {
	scheme, host string
	port         int
}

func originOf(u *url.URL) (origin, error) {
	port := 0
	if text := u.Port(); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n > 65535 {
			return origin{}, fmt.Errorf("invalid port %q", text)
		}
		port = n
	}
	if port == 0 {
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
	}
	return origin{u.Scheme, strings.ToLower(u.Hostname()), port}, nil
}

func unescape(path string) string {
	if out, err := url.PathUnescape(path); err == nil {
		return out
	}
	return path
}

// localPath returns the API suffix of a callback to this ConfigFlow for
// the captured profile (found=false for any other URL).
func (s *snapshot) localPath(ctx context.Context, raw, baseURL string) (string, bool, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	var paths []string
	if parsed.Scheme == "" && parsed.Host == "" {
		paths = append(paths, unescape(parsed.EscapedPath()))
	} else {
		from, err := originOf(parsed)
		if err != nil {
			return "", false, err
		}
		for _, base := range []string{s.SystemConfig.ServerDomain, baseURL} {
			if base == "" {
				continue
			}
			candidate, err := url.Parse(base)
			if err != nil {
				return "", false, err
			}
			expected, err := originOf(candidate)
			if err != nil {
				return "", false, err
			}
			prefix := strings.TrimRight(candidate.EscapedPath(), "/")
			if path := parsed.EscapedPath(); from == expected && strings.HasPrefix(path, prefix+"/") {
				paths = append(paths, unescape(path[len(prefix):]))
			}
		}
	}
	profileID := s.ProfileID
	if profileID == "" {
		if profileID, err = repo.GetDefaultProfileID(ctx); err != nil {
			return "", false, err
		}
	}
	for _, path := range paths {
		switch {
		case strings.HasPrefix(path, "/api/profiles/"):
			prefix := "/api/profiles/" + profileID + "/"
			if !strings.HasPrefix(path, prefix) {
				return "", false, archiveErr("规则回调引用了其他配置")
			}
			return "/" + path[len(prefix):], true, nil
		case strings.HasPrefix(path, "/api/"):
			return path[4:], true, nil
		case strings.HasPrefix(path, "/mosdns/"), strings.HasPrefix(path, "/rules/local/"),
			strings.HasPrefix(path, "/rule-library/content/"):
			return path, true, nil
		}
	}
	return "", false, nil
}

// source is where a rule file's bytes come from: an inline library rule or
// a remote URL with its declared identity.
type source struct {
	inline   bool
	content  string
	url      string
	identity string
}

// resolve follows callbacks to their source and returns the conversion
// parts to apply in order ("" = the default conversion).
func (s *snapshot) resolve(ctx context.Context, config *model.Config, raw, baseURL string, depth int, sourceID string) (source, []string, error) {
	if depth > 3 {
		return source{}, nil, archiveErr("规则回调存在循环引用")
	}
	if raw == "" {
		return source{}, nil, archiveErr("规则缺少下载地址")
	}
	local, found, err := s.localPath(ctx, raw, baseURL)
	if err != nil {
		return source{}, nil, err
	}
	if !found {
		declared, err := rulefetch.ResolveSource(raw, sourceConfigOf(config), sourceID)
		if err != nil {
			return source{}, nil, err
		}
		identity := sourceID
		if declared != nil && declared.ID != "" {
			identity = declared.ID
		}
		return source{url: raw, identity: identity}, nil, nil
	}
	if local == "/mosdns/rule-proxy" {
		parsed, _ := url.Parse(raw)
		query := parsed.Query()
		part := query.Get("part")
		if part != "" && part != PartDomain && part != PartIP {
			return source{}, nil, archiveErr("规则转换类型无效")
		}
		src, parts, err := s.resolve(ctx, config, query.Get("url"), baseURL, depth+1, query.Get("source_id"))
		return src, append(parts, part), err
	}
	libraryID, byID := strings.CutPrefix(local, "/rule-library/content/")
	name, byName := strings.CutPrefix(local, "/rules/local/")
	if !byID && !byName {
		return source{}, nil, archiveErr("无法识别规则回调")
	}
	for _, rule := range s.RuleLibrary {
		if !s.selected(rule.ID) || !(byID && rule.ID == libraryID || byName && rule.Name == name) {
			continue
		}
		if textValue(rule.SourceType, "url") == "content" {
			return source{inline: true, content: textValue(rule.Content, "")}, nil, nil
		}
		return s.resolve(ctx, config, textValue(rule.URL, ""), baseURL, depth+1, rule.ID)
	}
	return source{}, nil, archiveErr("规则不在当前配置快照中")
}

// detail is the user-visible part of a wrapped failure: the message of
// safe errors, otherwise only the error class (and HTTP status).
func detail(err error) string {
	var archive *ArchiveError
	var remote *rulefetch.RemoteRuleValidationError
	if errors.As(err, &archive) || errors.As(err, &remote) {
		return err.Error()
	}
	return safeDetails(err)
}

// safeDetails describes an error without its text (which could hold URLs
// or credentials): the error class and an HTTP status.
func safeDetails(err error) string {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return weburl.SafeErrorDetails(linkErr)
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, cause) {
			return weburl.SafeErrorDetails(cause)
		}
	}
	// These labels are part of the existing download failure message. Select
	// them by the source owner's typed failures, never a dynamic class method.
	class := "ValueError"
	var download *rulefetch.DownloadError
	var fetch *rulefetch.FetchError
	var ruleFetch *rulefetch.RuleFetchError
	var remote *rulefetch.RemoteRuleValidationError
	switch {
	case errors.Is(err, errNotUTF8):
		class = "UnicodeDecodeError"
	case errors.As(err, &download):
		class = "RequestException"
	case errors.As(err, &ruleFetch):
		class = "RuleFetchError"
	case errors.As(err, &remote):
		class = "RemoteRuleValidationError"
	case errors.As(err, &fetch):
		class = fetch.Class
	}
	details := "exception_type=" + class
	if errors.As(err, &fetch) && fetch.Status != 0 && !errors.As(err, &ruleFetch) {
		details += " status=" + strconv.Itoa(fetch.Status)
	}
	return details
}

type use struct {
	name, path string
	parts      []string
}

type fetched struct {
	key     source
	content string
	err     error
}

// BuildZIP packs config.yaml, the custom files and every download
// into a ZIP. config is the profile snapshot (it is not modified).
func BuildZIP(ctx context.Context, config *model.Config, yamlContent string, downloads []Download, custom []File, opts ArchiveOptions) ([]byte, error) {
	// Ignore client disconnects while retaining the Runtime's shutdown bound.
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config == nil || config.Profile == nil {
		return nil, archiveErr("MosDNS 配置快照缺失")
	}
	snap := snapshotOf(config)
	files := map[string][]byte{}
	var order []string
	total := 0
	add := func(path string, content []byte) error {
		path, err := ArchivePath(path)
		if err != nil {
			return err
		}
		if existing, ok := files[path]; ok {
			// Separate routing entries may consume the same library file.
			if path != "config.yaml" && bytes.Equal(existing, content) {
				return nil
			}
			return archiveErr("生成配置包含内容冲突的文件路径")
		}
		total += len(content)
		if len(files) >= MaxFiles || total > MaxTotalBytes {
			return archiveErr("生成配置超过打包大小限制")
		}
		files[path] = content
		order = append(order, path)
		return nil
	}
	if err := add("config.yaml", []byte(yamlContent)); err != nil {
		return nil, err
	}
	for _, file := range custom {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := add(file.Path, []byte(file.Content)); err != nil {
			return nil, err
		}
	}

	// Entry order: config.yaml, custom files, downloads as declared.
	planned := map[string]bool{}
	for _, path := range order {
		planned[path] = true
	}
	uses := map[source][]use{}
	var sources []source
	for _, d := range downloads {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := ArchivePath(d.LocalPath)
		var src source
		var parts []string
		if err == nil {
			src, parts, err = snap.resolve(ctx, config, d.URL, opts.BaseURL, 0, "")
		}
		if err != nil {
			return nil, archiveErr("规则“" + label(d.Name) + "”准备失败：" + detail(err))
		}
		if !planned[path] {
			planned[path] = true
			order = append(order, path)
			if len(planned) > MaxFiles {
				return nil, archiveErr("生成配置超过打包文件数量限制")
			}
		}
		if _, seen := uses[src]; !seen {
			sources = append(sources, src)
		}
		uses[src] = append(uses[src], use{name: d.Name, path: path, parts: parts})
	}
	entries := order
	order = nil

	// Downloads may outlive an early failure, so Runtime tracks each worker.
	// At most MaxWorkers downloads stay in flight; results are buffered.
	fetch := captureFetch()
	results := make(chan fetched, len(sources))
	next, pending := 0, 0
	schedule := func() error {
		for pending < MaxWorkers && next < len(sources) && ctx.Err() == nil {
			src := sources[next]
			if err := lifecycle.SpawnerFrom(ctx).Spawn(ctx, "mosdns-archive-download", func(work context.Context) {
				result := fetched{key: src, err: lifecycle.ErrPanic}
				defer func() { results <- result }()
				result.content, result.err = fetchSource(work, opts.Repo, fetch, config, src, label(uses[src][0].name))
			}); err != nil {
				return err
			}
			next++
			pending++
		}
		return nil
	}
	if err := schedule(); err != nil {
		return nil, err
	}
	for ; pending > 0; pending-- {
		var result fetched
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result = <-results:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.err != nil {
			return nil, result.err
		}
		for _, u := range uses[result.key] {
			content := result.content
			var err error
			for _, part := range u.parts {
				if part == "" {
					content, err = ConvertRuleContentForMosDNS(content)
				} else {
					content, err = ConvertRuleContentForMosDNS(content, part)
				}
				if err != nil {
					break
				}
			}
			if err == nil {
				err = add(u.path, []byte(content))
			}
			if err != nil {
				return nil, archiveErr("规则“" + label(u.name) + "”转换失败：" + detail(err))
			}
		}
		if err := schedule(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkReferences(yamlContent, files, opts.SkipReferenceValidation); err != nil {
		return nil, err
	}
	data, err := writeZip(entries, files)
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	return data, err
}

func sourceConfigOf(config *model.Config) *rulefetch.SourceConfig {
	if config == nil {
		return nil
	}
	var ruleConfigs []model.RuleConfig
	if config.Profile != nil {
		ruleConfigs = config.Profile.RuleConfigs
	}
	return &rulefetch.SourceConfig{
		Settings: config.SystemConfig,
		Library:  config.RuleLibrary,
		Rules:    ruleConfigs,
	}
}

func libraryItemOf(rule model.RuleConfig) (model.RuleLibraryItem, error) {
	data, err := model.Marshal(rule)
	if err != nil {
		return model.RuleLibraryItem{}, err
	}
	var item model.RuleLibraryItem
	err = json.Unmarshal(data, &item)
	return item, err
}

// fetchSource returns a source's text: inline content, the remote
// declaration's validated cache or download, or an undeclared URL.
func fetchSource(ctx context.Context, r *repo.Repository, fetch nativeFetchFunc, config *model.Config, src source, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if src.inline {
		return src.content, nil
	}
	text, err := fetchDeclaredSource(ctx, r, fetch, config, src, name)

	if cancelled := ctx.Err(); cancelled != nil {
		return "", cancelled
	}
	if err != nil {
		return "", archiveErr("规则“" + name + "”下载失败：" + detail(err))
	}
	return text, nil
}

func fetchDeclaredSource(ctx context.Context, r *repo.Repository, fetch nativeFetchFunc, config *model.Config, src source, name string) (string, error) {
	sourceConfig := sourceConfigOf(config)
	declared, err := rulefetch.ResolveSource(src.url, sourceConfig, src.identity)
	if err != nil {
		return "", err
	}
	if declared == nil {
		content, err := fetch(ctx, src.url, config, "")
		return strings.ToValidUTF8(string(content), "\ufffd"), err
	}
	var library []model.RuleLibraryItem
	if config != nil {
		library = config.RuleLibrary
	}
	if format, err := rulesetfmt.RuleFormat(*declared, library, ""); err != nil {
		return "", err
	} else if format == "mrs" {
		return "", rulefetch.NewRemoteRuleMessage(mrsMessage)
	}
	item, err := libraryItemOf(*declared)
	if err != nil {
		return "", err
	}
	content, stale, err := refresh.New(refresh.Options{Repo: r}).ReadRemote(ctx, item, refresh.RuleReadOptions{
		Config: sourceConfig,
		Fetch: func(callCtx context.Context, target string) (rulefetch.Content, error) {
			data, err := fetch(callCtx, target, config, "")
			return rulefetch.Content{Data: data}, err
		},
	})
	if err != nil {
		return "", err
	}
	if stale != nil {
		archiveLogger.Warningf("MosDNS ZIP 规则“%s”更新失败，使用上一次有效缓存：%s", name, refresh.FailureReason(stale))
	}
	return decodeText(content)
}

const mrsMessage = "OxiDNS/MosDNS 不支持 MRS 二进制规则，请使用该订阅的 Text 或 YAML 版本。"

// errNotUTF8 is rule content that is not UTF-8.
var errNotUTF8 = errors.New("rule content is not valid UTF-8")

// decodeText decodes UTF-8 (a leading BOM is dropped).
func decodeText(content []byte) (string, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(content) {
		return "", errNotUTF8
	}
	return string(content), nil
}

// checkReferences verifies that config.yaml is a mapping with a plugin
// list and (unless skipped) that every plugin's args.files entry is packed.
func checkReferences(yamlContent string, files map[string][]byte, skip bool) error {
	document, err := yamlx.Parse([]byte(yamlContent))
	if err != nil {
		return err
	}
	plugins := yamlx.Resolve(yamlx.Get(document, "plugins"))
	if document.Kind != yaml.MappingNode || plugins != nil && plugins.Kind != yaml.SequenceNode {
		return archiveErr("MosDNS 配置格式无效")
	}
	if plugins == nil {
		return nil
	}
	for _, plugin := range plugins.Content {
		if skip {
			break
		}
		plugin = yamlx.Resolve(plugin)
		if plugin.Kind != yaml.MappingNode {
			return errors.New("MosDNS plugin is not a mapping")
		}
		args := yamlx.Get(plugin, "args")
		references := yamlx.Resolve(yamlx.Get(args, "files"))
		if references == nil {
			continue
		}
		if references.Kind != yaml.SequenceNode {
			return archiveErr("MosDNS 文件引用必须为列表")
		}
		for _, reference := range references.Content {
			reference = yamlx.Resolve(reference)
			text := ""
			if reference.Kind == yaml.ScalarNode && reference.ShortTag() == "!!str" {
				text = reference.Value
			}
			path, err := ArchivePath(text)
			if err != nil {
				return err
			}
			if _, packed := files[path]; !packed {
				return archiveErr("MosDNS 配置引用了未打包的规则文件")
			}
		}
	}
	return nil
}

func writeZip(order []string, files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	now := time.Now()
	for _, name := range order {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now}
		header.SetMode(0o600)
		// Writestr-style members carry private permissions without a redundant
		// regular-file type bit; both DNS archives share this wire metadata.
		header.ExternalAttrs = 0o600 << 16
		writer, err := archive.CreateHeader(header)
		if err == nil {
			_, err = writer.Write(files[name])
		}
		if err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
