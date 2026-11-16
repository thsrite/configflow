package mosdns

// Port of backend/utils/mosdns_archive.py: materialize complete MosDNS
// downloads without HTTP calls back to ConfigFlow.

import (
	"archive/zip"
	"bytes"
	"context"
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
)

var archiveLogger = logx.Get("backend.utils.mosdns_archive")

// MaxWorkers is MAX_WORKERS: concurrent source downloads of one archive.
const MaxWorkers = 8

// MaxFiles and MaxTotalBytes are MAX_FILES and MAX_TOTAL_BYTES (variables so
// tests can lower them like Python's monkeypatch).
var (
	MaxFiles      = 4096
	MaxTotalBytes = 256 * 1024 * 1024
)

// ArchiveError is MosdnsArchiveError (a ValueError): a safe, user-facing
// error that contains no source URL or credentials. OxidnsArchiveError is the
// same class.
type ArchiveError struct{ Message string }

func (e *ArchiveError) Error() string { return e.Message }

// PythonClass is type(exc).__name__.
func (e *ArchiveError) PythonClass() string { return "MosdnsArchiveError" }

func archiveError(message string) error { return &ArchiveError{Message: message} }

// ArchivePath is _path(value): a safe relative file path inside the
// archive, or *ArchiveError.
func ArchivePath(value any) (out string, err error) {
	defer catch(&err)
	return archivePath(value), nil
}

func archivePath(value any) string {
	text, ok := value.(string)
	if !ok || text == "" || strings.Contains(text, "\\") || hasControl(text) {
		raise(archiveError("规则文件路径无效"))
	}
	for strings.HasPrefix(text, "./") {
		text = text[2:]
	}
	if strings.HasPrefix(text, "/") {
		raise(archiveError("规则文件必须使用安全的相对路径"))
	}
	for _, part := range strings.Split(text, "/") {
		if part == "" || part == "." || part == ".." {
			raise(archiveError("规则文件必须使用安全的相对路径"))
		}
	}
	// PurePosixPath(text).as_posix() is text itself for every path that
	// passed the checks (no empty, "." or ".." parts, not absolute).
	return text
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 32 {
			return true
		}
	}
	return false
}

// archiveLabel is _label(item): the rule's name without pasted URLs,
// credential expressions or control characters, at most 160 characters.
func archiveLabel(item any) string {
	name := get(item, "name", nil)
	if !truthy(name) {
		name = "未命名规则"
	}
	text := str(name)
	lower := strings.ToLower(text)
	if strings.Contains(text, "://") || strings.Contains(lower, "token=") || strings.Contains(lower, "password=") ||
		strings.Contains(lower, "secret=") {
		return "未命名规则"
	}
	var b strings.Builder
	count := 0
	for _, r := range text {
		if r < 32 || r == 127 {
			continue
		}
		if count == 160 {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

// splitURL is urlsplit(value) on net/url: a non-string or a URL net/url
// refuses raises (Python fails on non-strings too).
func splitURL(value any) *url.URL {
	text, ok := value.(string)
	if !ok {
		raise(attributeError(value, "decode"))
	}
	return parseURL(text)
}

// originOf is (scheme, hostname, port or the scheme's default).
func originOf(u *url.URL) [3]string {
	port := 0
	if text := u.Port(); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n > 65535 {
			raise(pyErr("ValueError", "Port out of range 0-65535"))
		}
		port = n
	}
	if port == 0 {
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
	}
	return [3]string{u.Scheme, strings.ToLower(u.Hostname()), strconv.Itoa(port)}
}

// unquote is urllib's unquote on net/url (the text itself when it holds an
// invalid escape).
func unquote(text string) string {
	if out, err := url.PathUnescape(text); err == nil {
		return out
	}
	return text
}

// localPath is _local_path(url, config, base_url): the local API suffix
// only for this captured profile and origin, ok=false for None.
func localPath(ctx context.Context, rawURL string, config *jsonx.Object, baseURL string) (string, bool) {
	parsed := splitURL(rawURL)
	system := get(config, "system_config", jsonx.NewObject())
	bases := []any{get(system, "server_domain", ""), baseURL}
	var paths []string
	if parsed.Scheme == "" && parsed.Host == "" && parsed.User == nil {
		paths = append(paths, unquote(parsed.EscapedPath()))
	} else {
		origin := originOf(parsed)
		for _, base := range bases {
			if !truthy(base) {
				continue
			}
			candidate := splitURL(base)
			expected := originOf(candidate)
			prefix := strings.TrimRight(candidate.EscapedPath(), "/")
			path := parsed.EscapedPath()
			if origin == expected && strings.HasPrefix(path, prefix+"/") {
				paths = append(paths, unquote(path[len(prefix):]))
			}
		}
	}
	profileID := get(config, "profile_id", nil)
	if !truthy(profileID) {
		id, err := repo.GetDefaultProfileID(ctx)
		if err != nil {
			raise(err)
		}
		profileID = id
	}
	for _, path := range paths {
		if strings.HasPrefix(path, "/api/profiles/") {
			prefix := "/api/profiles/" + str(profileID) + "/"
			if !strings.HasPrefix(path, prefix) {
				raise(archiveError("规则回调引用了其他配置"))
			}
			return "/" + path[len(prefix):], true
		}
		if strings.HasPrefix(path, "/api/") {
			return path[4:], true
		}
		if strings.HasPrefix(path, "/mosdns/") || strings.HasPrefix(path, "/rules/local/") ||
			strings.HasPrefix(path, "/rule-library/content/") {
			return path, true
		}
	}
	return "", false
}

// archiveSource is the key of _source: ('remote', url, identity) or
// ('inline', content).
type archiveSource struct {
	inline   bool
	value    any // remote: the URL (string); inline: the content
	identity any // remote: the declared id or the source_id (nil: None)
	hash     string
}

func (s archiveSource) key() string {
	if s.inline {
		return "inline\x00" + hashKey(s.value)
	}
	return "remote\x00" + hashKey(s.value) + "\x00" + hashKey(s.identity)
}

// sourceIDText is the source_id argument resolve_remote_rule_source sees:
// `if source_id:` then str(source_id); "" stands for a falsy value.
func sourceIDText(sourceID any) string {
	if !truthy(sourceID) {
		return ""
	}
	return str(sourceID)
}

// archiveSourceOf is _source(url, config, base_url, depth, source_id):
// callbacks resolved to inline text or a remote source plus the conversion
// parts (nil = None) applied in order.
func archiveSourceOf(ctx context.Context, rawURL any, config *jsonx.Object, baseURL string, depth int, sourceID any) (archiveSource, []any) {
	if depth > 3 {
		raise(archiveError("规则回调存在循环引用"))
	}
	text, ok := rawURL.(string)
	if !ok || text == "" {
		raise(archiveError("规则缺少下载地址"))
	}
	local, found := localPath(ctx, text, config, baseURL)
	if !found {
		source, err := rulefetch.ResolveRemoteRuleSource(text, config, sourceIDText(sourceID))
		if err != nil {
			raise(err)
		}
		var identity any
		if source != nil {
			identity = source.Value("id")
		}
		if !truthy(identity) {
			identity = sourceID
		}
		return archiveSource{value: text, identity: identity}, nil
	}
	if local == "/mosdns/rule-proxy" {
		query, _ := url.ParseQuery(splitURL(text).RawQuery)
		original := query.Get("url")
		var part any
		if value := query.Get("part"); value != "" {
			part = value
		}
		if part != nil && !inList(part.(string), RuleParts) {
			raise(archiveError("规则转换类型无效"))
		}
		var nestedID any
		if value := query.Get("source_id"); value != "" {
			nestedID = value
		}
		key, conversions := archiveSourceOf(ctx, original, config, baseURL, depth+1, nestedID)
		return key, append(conversions, part)
	}
	var libraryID, name *string
	if rest, ok := strings.CutPrefix(local, "/rule-library/content/"); ok {
		libraryID = &rest
	}
	if rest, ok := strings.CutPrefix(local, "/rules/local/"); ok {
		name = &rest
	}
	if libraryID == nil && name == nil {
		raise(archiveError("无法识别规则回调"))
	}
	selected := selectedLibraryIDs(config)
	for _, rule := range iterate(get(config, "rule_library", []any{})) {
		if !selected.has(get(rule, "id", nil)) {
			continue
		}
		if (libraryID != nil && jsonx.Equal(get(rule, "id", nil), *libraryID)) ||
			(name != nil && jsonx.Equal(get(rule, "name", nil), *name)) {
			if jsonx.Equal(get(rule, "source_type", nil), "content") {
				return archiveSource{inline: true, value: get(rule, "content", "")}, nil
			}
			return archiveSourceOf(ctx, get(rule, "url", ""), config, baseURL, depth+1, get(rule, "id", nil))
		}
	}
	raise(archiveError("规则不在当前配置快照中"))
	return archiveSource{}, nil
}

// selectedLibraryIDs is {rule.get('library_rule_id') for rule in
// config.get('rule_configs', []) if rule.get('enabled', True) and
// rule.get('library_enabled', True)}.
func selectedLibraryIDs(config any) pySet {
	selected := pySet{}
	for _, rule := range iterate(get(config, "rule_configs", []any{})) {
		if truthy(get(rule, "enabled", true)) && truthy(get(rule, "library_enabled", true)) {
			selected[hashKey(get(rule, "library_rule_id", nil))] = true
		}
	}
	return selected
}

// ArchiveOptions are the keyword arguments of build_mosdns_zip.
type ArchiveOptions struct {
	// BaseURL is base_url (default '').
	BaseURL string
	// SkipReferenceValidation is validate_references=False (the OxiDNS
	// archive); the zero value validates like Python's default.
	SkipReferenceValidation bool
}

// isArchiveDetailError is isinstance(exc, (MosdnsArchiveError,
// RemoteRuleValidationError)).
func isArchiveDetailError(err error) bool {
	switch err.(type) {
	case *ArchiveError, *rulefetch.RemoteRuleValidationError:
		return true
	}
	return false
}

func archiveDetail(err error) string {
	if isArchiveDetailError(err) {
		return err.Error()
	}
	return safeExceptionDetails(err)
}

type archiveUse struct {
	item        any
	path        string
	conversions []any
}

type archiveFetchResult struct {
	key     string
	content any
	err     error
}

// BuildMosDNSZip is build_mosdns_zip(config, yaml_content, downloads,
// custom_files, base_url=, validate_references=): fetch each original URL
// once (at most MaxWorkers at a time), convert each part, then create the
// ZIP. downloads and customFiles are lists of objects (as Python iterates
// them; other items fail like Python). The result is the ZIP bytes;
// failures are *ArchiveError or the error Python lets through.
func BuildMosDNSZip(ctx context.Context, config *jsonx.Object, yamlContent string, downloads, customFiles []any, opts ArchiveOptions) (out []byte, err error) {
	defer catch(&err)
	// Downloads, cache writes and task records finish even when the caller
	// goes away, as Python's worker threads do.
	ctx = context.WithoutCancel(ctx)
	// Workers must never select a default profile or read a changed setting.
	config = jsonx.DeepCopy(config).(*jsonx.Object)
	files := map[string][]byte{}
	var fileOrder []string
	inOrder := map[string]bool{}
	total := 0
	maxFiles, maxTotal := MaxFiles, MaxTotalBytes

	add := func(path any, content any) {
		name := archivePath(path)
		var value []byte
		switch c := content.(type) {
		case string:
			value = []byte(c)
		case []byte:
			value = c
		default:
			raise(archiveError("规则文件缺少内容"))
		}
		if existing, present := files[name]; present {
			// Separate routing entries may consume the same library file.
			if name != "config.yaml" && bytes.Equal(existing, value) {
				return
			}
			raise(archiveError("生成配置包含内容冲突的文件路径"))
		}
		total += len(value)
		if len(files) >= maxFiles || total > maxTotal {
			raise(archiveError("生成配置超过打包大小限制"))
		}
		files[name] = value
	}

	add("config.yaml", yamlContent)
	for _, item := range customFiles {
		add(get(item, "path", nil), get(item, "content", nil))
	}
	for name := range files {
		inOrder[name] = true
	}
	fileOrder = append(fileOrder, "config.yaml")
	for _, item := range customFiles {
		// dict.fromkeys(files): insertion order of the paths added above.
		name := archivePath(get(item, "path", nil))
		if !contains2(fileOrder, name) {
			fileOrder = append(fileOrder, name)
		}
	}

	sources := map[string][]archiveUse{}
	keys := map[string]archiveSource{}
	var sourceOrder []string
	for _, item := range downloads {
		var (
			path        string
			source      archiveSource
			conversions []any
		)
		prepareErr := func() (err error) {
			defer catch(&err)
			path = archivePath(get(item, "local_path", nil))
			source, conversions = archiveSourceOf(ctx, get(item, "url", nil), config, opts.BaseURL, 0, nil)
			return nil
		}()
		if prepareErr != nil {
			raise(archiveError("规则“" + archiveLabel(item) + "”准备失败：" + archiveDetail(prepareErr)))
		}
		if !inOrder[path] {
			inOrder[path] = true
			fileOrder = append(fileOrder, path)
		}
		if len(fileOrder) > maxFiles {
			raise(archiveError("生成配置超过打包文件数量限制"))
		}
		key := source.key()
		if _, present := sources[key]; !present {
			sourceOrder = append(sourceOrder, key)
			keys[key] = source
		}
		sources[key] = append(sources[key], archiveUse{item: item, path: path, conversions: conversions})
	}

	fetch := func(key string) (any, error) {
		source := keys[key]
		if source.inline {
			return source.value, nil
		}
		label := func() string { return archiveLabel(sources[key][0].item) }
		content, err := fetchArchiveSource(ctx, source, config, label)
		if err != nil {
			return nil, archiveError("规则“" + label() + "”下载失败：" + archiveDetail(err))
		}
		return content, nil
	}

	results := make(chan archiveFetchResult, len(sourceOrder))
	next, pending := 0, 0
	schedule := func() {
		for pending < MaxWorkers && next < len(sourceOrder) {
			key := sourceOrder[next]
			next++
			pending++
			go func() {
				var result archiveFetchResult
				result.key = key
				defer func() {
					if r := recover(); r != nil {
						if p, ok := r.(raised); ok {
							result.err = p.err
						} else {
							result.err = pyErr("Exception", "worker panic")
						}
					}
					results <- result
				}()
				result.content, result.err = fetch(key)
			}()
		}
	}
	// A failure returns at once: at most MaxWorkers downloads remain in
	// flight with bounded timeouts; their results are discarded (Python's
	// shutdown(wait=False, cancel_futures=True)).
	schedule()
	for pending > 0 {
		result := <-results
		pending--
		if result.err != nil {
			raise(result.err)
		}
		for _, use := range sources[result.key] {
			convertErr := func() (err error) {
				defer catch(&err)
				content := result.content
				for _, part := range use.conversions {
					content = convertForArchive(content, part)
				}
				add(use.path, content)
				return nil
			}()
			if convertErr != nil {
				raise(archiveError("规则“" + archiveLabel(use.item) + "”转换失败：" + archiveDetail(convertErr)))
			}
		}
		schedule()
	}

	document, loadErr := yamlx.LoadWith(yamlContent, yamlx.LoadOptions{KeepNonStringKeys: true})
	if loadErr != nil {
		raise(loadErr)
	}
	if !isDict(document) {
		raise(archiveError("MosDNS 配置格式无效"))
	}
	plugins := get(document, "plugins", []any{})
	pluginList, isList := plugins.([]any)
	if !isList {
		raise(archiveError("MosDNS 配置格式无效"))
	}
	if !opts.SkipReferenceValidation {
		for _, plugin := range pluginList {
			args := get(plugin, "args", nil)
			if !truthy(args) {
				args = jsonx.NewObject()
			}
			if !isDict(args) {
				continue
			}
			references, isRefList := get(args, "files", []any{}).([]any)
			if !isRefList {
				raise(archiveError("MosDNS 文件引用必须为列表"))
			}
			for _, reference := range references {
				if _, present := files[archivePath(reference)]; !present {
					raise(archiveError("MosDNS 配置引用了未打包的规则文件"))
				}
			}
		}
	}
	return writeZip(fileOrder, files)
}

func contains2(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func isDict(v any) bool {
	switch x := v.(type) {
	case *jsonx.Object:
		return x != nil
	case *yamlx.Mapping:
		return true
	}
	return false
}

// convertForArchive is convert_rule_content_for_mosdns(content, part) on
// what a fetch returned (str, or bytes from a replaced fetch).
func convertForArchive(content any, part any) any {
	text, ok := content.(string)
	if !ok {
		if _, isBytes := content.([]byte); isBytes {
			raise(pyErr("TypeError", "a bytes-like object is required, not 'str'"))
		}
		raise(attributeError(content, "removeprefix"))
	}
	var out string
	var err error
	if part == nil {
		out, err = ConvertRuleContentForMosDNS(text)
	} else {
		out, err = ConvertRuleContentForMosDNS(text, part.(string))
	}
	if err != nil {
		raise(err)
	}
	return out
}

// fetchArchiveSource is the remote branch of build_mosdns_zip's fetch:
// rule-proxy's bounded transport and cache fallback on the captured
// snapshot (no request context: no host URL).
func fetchArchiveSource(ctx context.Context, source archiveSource, config *jsonx.Object, label func() string) (out any, err error) {
	defer catch(&err)
	sourceURL := source.value.(string)
	declared, err := rulefetch.ResolveRemoteRuleSource(sourceURL, config, sourceIDText(source.identity))
	if err != nil {
		return nil, err
	}
	if declared == nil {
		return currentEnv().fetchRemoteContent(ctx, sourceURL, config, false, "")
	}
	format, err := convert.RulesetFormat(declared, config, "")
	if err != nil {
		return nil, err
	}
	if format == "mrs" {
		return nil, rulefetch.NewRemoteRuleMessage("OxiDNS/MosDNS 不支持 MRS 二进制规则，请使用该订阅的 Text 或 YAML 版本。")
	}
	content, staleErr, err := refresh.ReadRuleSource(ctx, declared, refresh.RuleOptions{Config: config,
		Fetch: func(fetchURL string) (any, error) {
			return currentEnv().fetchRemoteContent(ctx, fetchURL, config, true, "")
		}})
	if err != nil {
		return nil, err
	}
	if staleErr != nil {
		archiveLogger.Warningf("MosDNS ZIP 规则“%s”更新失败，使用上一次有效缓存：%s", label(), refresh.DescribeFailure(staleErr))
	}
	return decodeUTF8Sig(content)
}

// decodeUTF8Sig is bytes.decode('utf-8-sig') (UnicodeDecodeError).
func decodeUTF8Sig(content []byte) (string, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(content) {
		return "", pyErr("UnicodeDecodeError", "'utf-8' codec can't decode the rule content")
	}
	return string(content), nil
}

// writeZip writes the archive like zipfile.ZipFile(..., ZIP_DEFLATED) with
// writestr: deflated entries, mode 0600, the local time of writing.
func writeZip(order []string, files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	now := time.Now()
	for _, name := range order {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now}
		header.SetMode(0o600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
