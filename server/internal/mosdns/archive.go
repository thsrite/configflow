package mosdns

// MosDNS ZIP archive (backend/utils/mosdns_archive.py): every rule file is
// materialized from the captured profile snapshot without HTTP calls back
// to ConfigFlow; each remote source is fetched once, at most MaxWorkers at
// a time.

import (
	"archive/zip"
	"bytes"
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
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
	URL       string `json:"url"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
}

// File is a file stored as is (GetMosDNSCustomFiles, OxiDNS attachments).
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// DecodeList converts a converter result list (downloads, custom files).
func DecodeList[T any](list []any) ([]T, error) {
	var out []T
	return out, viaJSON(list, &out)
}

func viaJSON(value any, target any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// ArchiveOptions are the options of BuildMosDNSZip.
type ArchiveOptions struct {
	BaseURL string
	// SkipReferenceValidation skips the check that every `files` entry of
	// the config is packed (the OxiDNS archive).
	SkipReferenceValidation bool
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

// snapshot is the part of the profile snapshot the callbacks resolve
// against. Hand-edited values of another JSON type are kept and compared
// like Python compares them (a number never equals a string).
type snapshot struct {
	ProfileID    text `json:"profile_id"`
	SystemConfig struct {
		ServerDomain   text `json:"server_domain"`
		ConfigToken    text `json:"config_token"`
		RuleProxyToken text `json:"rule_proxy_token"`
	} `json:"system_config"`
	RuleConfigs []struct {
		LibraryRuleID  text  `json:"library_rule_id"`
		Enabled        truth `json:"enabled"`
		LibraryEnabled truth `json:"library_enabled"`
	} `json:"rule_configs"`
	RuleLibrary []struct {
		ID         text `json:"id"`
		Name       text `json:"name"`
		SourceType text `json:"source_type"`
		URL        text `json:"url"`
		Content    text `json:"content"`
	} `json:"rule_library"`
}

// text is a snapshot value: a string (isString) or the JSON text of any
// other value; missing and null are the zero value.
type text struct {
	value    string
	isString bool
}

func (t *text) UnmarshalJSON(data []byte) error {
	if json.Unmarshal(data, &t.value) == nil {
		t.isString = true
	} else if string(data) != "null" {
		t.value = string(data)
	}
	return nil
}

func (t text) is(s string) bool { return t.isString && t.value == s }

// str is the string value ("" for other types).
func (t text) str() string {
	if t.isString {
		return t.value
	}
	return ""
}

// truth is a flag that defaults to true when missing and otherwise follows
// Python truthiness (null, 0, "" and empty containers are false).
type truth struct{ off bool }

func (t *truth) UnmarshalJSON(data []byte) error {
	var v any
	json.Unmarshal(data, &v)
	switch x := v.(type) {
	case []any:
		t.off = len(x) == 0
	case map[string]any:
		t.off = len(x) == 0
	default:
		t.off = v == nil || v == false || v == 0.0 || v == ""
	}
	return nil
}

// selected reports whether a library rule is used by an enabled rule
// configuration of the profile.
func (s *snapshot) selected(id text) bool {
	for _, rule := range s.RuleConfigs {
		if rule.LibraryRuleID == id && !rule.Enabled.off && !rule.LibraryEnabled.off {
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
		for _, base := range []string{s.SystemConfig.ServerDomain.str(), baseURL} {
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
	profileID := s.ProfileID.value
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
func (s *snapshot) resolve(ctx context.Context, config *jsonx.Object, raw, baseURL string, depth int, sourceID string) (source, []string, error) {
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
		declared, err := rulefetch.ResolveRemoteRuleSource(raw, config, sourceID)
		if err != nil {
			return source{}, nil, err
		}
		identity := sourceID
		if declared != nil && jsonx.Truthy(declared.Value("id")) {
			identity = jsonx.Str(declared.Value("id"))
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
		if !s.selected(rule.ID) || !(byID && rule.ID.is(libraryID) || byName && rule.Name.is(name)) {
			continue
		}
		if rule.SourceType.is("content") {
			return source{inline: true, content: rule.Content.str()}, nil, nil
		}
		return s.resolve(ctx, config, rule.URL.str(), baseURL, depth+1, rule.ID.value)
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
	class := rulefetch.PythonClass(err) // shown to users: Python's names
	if errors.Is(err, errNotUTF8) {
		class = "UnicodeDecodeError"
	}
	details := "exception_type=" + class
	var fetch *rulefetch.FetchError
	var ruleFetch *rulefetch.RuleFetchError
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

// BuildMosDNSZip packs config.yaml, the custom files and every download
// into a ZIP. config is the profile snapshot (it is not modified).
func BuildMosDNSZip(ctx context.Context, config *jsonx.Object, yamlContent string, downloads []Download, custom []File, opts ArchiveOptions) ([]byte, error) {
	// Downloads, cache writes and task records finish even when the caller
	// goes away.
	ctx = lifecycle.Detach(ctx)
	config = config.DeepCopy()
	var snap snapshot
	if err := viaJSON(config, &snap); err != nil {
		return nil, err
	}
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

	// A failure returns at once; at most MaxWorkers downloads stay in
	// flight (bounded timeouts) and their results are dropped.
	fetch := fetchRemoteFn
	results := make(chan fetched, len(sources))
	next, pending := 0, 0
	schedule := func() {
		for ; pending < MaxWorkers && next < len(sources); next, pending = next+1, pending+1 {
			src := sources[next]
			go func() {
				content, err := fetchSource(ctx, fetch, config, src, label(uses[src][0].name))
				results <- fetched{src, content, err}
			}()
		}
	}
	schedule()
	for ; pending > 0; pending-- {
		result := <-results
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
		schedule()
	}
	if err := checkReferences(yamlContent, files, opts.SkipReferenceValidation); err != nil {
		return nil, err
	}
	return writeZip(entries, files)
}

// fetchSource returns a source's text: inline content, the remote
// declaration's validated cache or download, or an undeclared URL.
func fetchSource(ctx context.Context, fetch fetchFunc, config *jsonx.Object, src source, name string) (string, error) {
	if src.inline {
		return src.content, nil
	}
	text, err := func() (string, error) {
		declared, err := rulefetch.ResolveRemoteRuleSource(src.url, config, src.identity)
		if err != nil {
			return "", err
		}
		if declared == nil {
			content, err := fetch(ctx, src.url, config, "")
			return strings.ToValidUTF8(string(content), "�"), err
		}
		if format, err := convert.RulesetFormat(declared, config, ""); err != nil {
			return "", err
		} else if format == "mrs" {
			return "", rulefetch.NewRemoteRuleMessage(mrsMessage)
		}
		content, stale, err := refresh.ReadRuleSource(ctx, declared, refresh.RuleOptions{Config: config,
			Fetch: func(url string) (any, error) { return fetch(ctx, url, config, "") }})
		if err != nil {
			return "", err
		}
		if stale != nil {
			archiveLogger.Warningf("MosDNS ZIP 规则“%s”更新失败，使用上一次有效缓存：%s", name, refresh.DescribeFailure(stale))
		}
		return decodeText(content)
	}()
	if err != nil {
		return "", archiveErr("规则“" + name + "”下载失败：" + detail(err))
	}
	return text, nil
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
