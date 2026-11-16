package oxidns

import (
	"encoding/base64"
	"math"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Port of backend/common/oxidns_native.py (module C22): bounded native
// OxiDNS documents and explicitly managed deployment assets.

const (
	// MaxNativeBytes is MAX_NATIVE_BYTES.
	MaxNativeBytes = 2 * 1024 * 1024
	// MaxAdditionalFiles is MAX_ADDITIONAL_FILES.
	MaxAdditionalFiles = 256
	// MaxAdditionalFileBytes is MAX_ADDITIONAL_FILE_BYTES.
	MaxAdditionalFileBytes = 16 * 1024 * 1024
	// MaxAdditionalTotalBytes is MAX_ADDITIONAL_TOTAL_BYTES.
	MaxAdditionalTotalBytes = 64 * 1024 * 1024
)

// Tag is TAG: a plugin tag (dot-separated ASCII labels). Callers also check
// the length (1–64) and the reserved qs.exec./qs.match./qs.cron. prefixes.
var Tag = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*$`)

// nativeKey is NativeLoader.construct_mapping's key check.
func nativeKey(key any, duplicate bool) error {
	text, ok := key.(string)
	if !ok || text == "__proto__" || text == "prototype" || text == "constructor" {
		return settingsError("OxiDNS YAML 字段名必须为安全文本")
	}
	if duplicate {
		return settingsError("OxiDNS YAML 包含重复字段")
	}
	return nil
}

// LoadNativeYAML is load_native_yaml(text, label): a native OxiDNS YAML
// document (NativeLoader: YAML 1.2 style booleans and numbers, safe string
// keys without duplicates), bounded in size (2 MiB, 50000 nodes, depth 32),
// plain values only, with a mapping at the top. Python's default label is
// DefaultNativeLabel.
//
// The document is loaded with yamlx.SchemaNative. Errors: *Error
// (OxidnsSettingsError) as in Python; Python's own non-YAML exceptions of
// PyYAML constructors (KeyError, TypeError ...) as *convert.PyError.
//
// Difference: PyYAML builds sets (!!set) and tuples (!!omap, !!pairs), and
// collections that contain themselves through an alias, that jsonx cannot
// hold; they are reported with the message Python gives when it reaches them
// ("只支持普通对象、数组和标量" / "不支持循环别名"), even where Python's
// traversal would first stop at another limit of the same document.
func LoadNativeYAML(text any, label string) (document *jsonx.Object, err error) {
	defer catch(&err)
	return loadNativeYAML(text, label), nil
}

// DefaultNativeLabel is load_native_yaml's default label.
const DefaultNativeLabel = "完整配置"

func loadNativeYAML(text any, label string) *jsonx.Object {
	source, ok := text.(string)
	if !ok || len(source) > MaxNativeBytes {
		fail("OxiDNS " + label + "必须为不超过 2 MiB 的 YAML 文本")
	}
	value, err := yamlx.LoadWith(source, yamlx.LoadOptions{Schema: yamlx.SchemaNative, Key: nativeKey})
	if err != nil {
		switch e := err.(type) {
		case *Error:
			raise(e)
		case *yamlx.Error:
			if e.IsYAMLError() || e.Class == "RecursionError" || e.Class == "ValueError" {
				fail("OxiDNS " + label + " YAML 语法无效")
			}
			raise(pyErr(e.Class, e.Msg))
		case *yamlx.UnrepresentableError:
			if e.Kind == "recursive" {
				fail("OxiDNS YAML 不支持循环别名")
			}
			fail("OxiDNS YAML 只支持普通对象、数组和标量")
		}
		raise(err)
	}
	count := 0
	var visit func(item any, depth int)
	visit = func(item any, depth int) {
		count++
		if count > 50000 || depth > 32 {
			fail("OxiDNS YAML 超出结构大小限制")
		}
		switch x := item.(type) {
		case *jsonx.Object:
			for _, child := range x.Values() {
				visit(child, depth+1)
			}
		case []any:
			for _, child := range x {
				visit(child, depth+1)
			}
		case nil, string, bool:
		case jsonx.Number:
			if x.IsFloat() {
				f := x.Float64()
				if math.IsInf(f, 0) || math.IsNaN(f) || x == "NaN" || x == "Infinity" || x == "-Infinity" {
					fail("OxiDNS YAML 数字必须为有限值")
				}
			}
		default:
			fail("OxiDNS YAML 只支持普通对象、数组和标量")
		}
	}
	visit(value, 0)
	document, ok := value.(*jsonx.Object)
	if !ok || document == nil {
		fail("OxiDNS " + label + "顶层必须为对象")
	}
	return document
}

// ManagedFile is one entry of managed_files: the normalized path and the
// exact bytes to write (Python's {'path', 'content': bytes, 'native': True}).
type ManagedFile struct {
	Path    string
	Content []byte
}

// Object is the Python dict of the entry ({path, content, native: true});
// content is []byte.
func (f ManagedFile) Object() *jsonx.Object {
	return jsonx.Obj("path", f.Path, "content", f.Content, "native", true)
}

// ManagedFiles is managed_files(value): validates the additional_files
// settings metadata and returns byte-exact materializer inputs.
func ManagedFiles(value any) (files []ManagedFile, err error) {
	defer catch(&err)
	return managedFiles(value), nil
}

var base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)

func managedFiles(value any) []ManagedFile {
	list, ok := value.([]any)
	if !ok || len(list) > MaxAdditionalFiles {
		fail("附加文件必须为数组，最多 256 个文件")
	}
	result := []ManagedFile{}
	var seen []string
	seenSet := map[string]bool{}
	total := 0
	for _, raw := range list {
		if !isDict(raw) {
			fail("附加文件条目必须为对象")
		}
		entry := raw
		p := safeRelativePath(get(entry, "path", nil), "附加文件路径")
		if p == "config.yaml" || p == "manifest.json" || p == "state" ||
			strings.HasPrefix(p, "state/") || strings.HasPrefix(p, ".configflow") {
			fail("附加文件不能覆盖主配置、部署元数据或 state/ 运行数据")
		}
		if seenSet[p] {
			fail("附加文件路径重复")
		}
		for _, previous := range seen {
			if strings.HasPrefix(p, previous+"/") || strings.HasPrefix(previous, p+"/") {
				fail("附加文件路径存在文件与目录冲突")
			}
		}
		seen = append(seen, p)
		seenSet[p] = true
		content, isText := get(entry, "content", nil).(string)
		encoding := get(entry, "encoding", "text")
		if !isText || !strIn(encoding, "text", "base64") {
			fail("附加文件内容必须为文本，编码为 text 或 base64")
		}
		if utf8.RuneCountInString(content) > MaxAdditionalFileBytes*2 {
			fail("单个附加文件不能超过 16 MiB")
		}
		var data []byte
		if encoding == "text" {
			data = []byte(content)
		} else {
			decoded, ok := b64decodeValidate(content)
			if !ok {
				fail("附加文件 Base64 内容无效")
			}
			data = decoded
		}
		if len(data) > MaxAdditionalFileBytes {
			fail("单个附加文件不能超过 16 MiB")
		}
		total += len(data)
		if total > MaxAdditionalTotalBytes {
			fail("附加文件总大小不能超过 64 MiB")
		}
		result = append(result, ManagedFile{Path: p, Content: data})
	}
	return result
}

// b64decodeValidate is base64.b64decode(text, validate=True) of CPython
// 3.11: only the standard alphabet with at most two trailing "=", then
// binascii.a2b_base64 in non-strict mode (a "=" right after a complete
// group is skipped, trailing bits are ignored).
func b64decodeValidate(text string) ([]byte, bool) {
	if !base64Pattern.MatchString(text) {
		return nil, false
	}
	data := strings.TrimRight(text, "=")
	pads := len(text) - len(data)
	switch len(data) % 4 {
	case 1:
		return nil, false
	case 2:
		if pads < 2 {
			return nil, false
		}
	case 3:
		if pads < 1 {
			return nil, false
		}
	}
	decoded, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// NativeSources is the include metadata native_document reports through its
// sources argument: the origin of every expanded plugin ({source, index})
// and the globals of each included file (name, mapping without plugins).
type NativeSources struct {
	Plugins  []any
	Includes []NativeInclude
}

// NativeInclude is one (name, globals) pair of NativeSources.Includes.
type NativeInclude struct {
	Name    string
	Globals *jsonx.Object
}

// Object is the Python sources dict ({'plugins': [...], 'includes':
// [[name, globals], ...]}).
func (s *NativeSources) Object() *jsonx.Object {
	includes := []any{}
	for _, include := range s.Includes {
		includes = append(includes, []any{include.Name, include.Globals})
	}
	return jsonx.Obj("plugins", s.Plugins, "includes", includes)
}

// NativeDocument is native_document(settings, validate_arguments=validate,
// sources=sources): the native YAML with its includes expanded the way the
// OxiDNS core does (included plugins first, included globals ignored). When
// sources is not nil it receives the include metadata. With validate the
// expanded document is checked by validate_expanded_document (module C23)
// and an OxidnsValidationError is returned for errors.
//
// Structure errors are *Error with Issues ({path, source, message,
// severity, code}).
func NativeDocument(settings *jsonx.Object, validate bool, sources *NativeSources) (document *jsonx.Object, err error) {
	defer catch(&err)
	return nativeDocument(settings, validate, sources), nil
}

func structureError(message, source string, at []any) *Error {
	if at == nil {
		at = []any{}
	}
	issue := jsonx.Obj("path", at, "source", source, "message", message, "severity", "error", "code", "structure")
	return &Error{Class: ClassSettings, Message: message, Issues: []any{issue}, HasIssues: true}
}

func nativeDocument(settings any, validate bool, sources *NativeSources) *jsonx.Object {
	assets := map[string][]byte{}
	for _, file := range managedFiles(get(settings, "additional_files", []any{})) {
		assets[file.Path] = file.Content
	}
	count := 0
	origins := []any{}
	includedGlobals := []NativeInclude{}

	var expand func(document *jsonx.Object, name string, parents []string) *jsonx.Object
	expand = func(document *jsonx.Object, name string, parents []string) *jsonx.Object {
		count++
		if len(parents) > 8 || count > 512 {
			raise(structureError("OxiDNS include 超过 8 层或文件数量限制", name, []any{"include"}))
		}
		var includesValue any = []any{}
		if value, present := document.Pop("include"); present {
			includesValue = value
		}
		references, ok := stringList(includesValue)
		if !ok {
			raise(structureError("OxiDNS include 必须为文件路径数组", name, []any{"include"}))
		}
		plugins, ok := document.GetOr("plugins", []any{}).([]any)
		if !ok {
			raise(structureError("OxiDNS plugins 必须为数组", name, []any{"plugins"}))
		}
		result := []any{}
		for index, reference := range references {
			if reference == "" || strings.HasPrefix(reference, "/") || strings.Contains(reference, "${") ||
				strings.Contains(reference, `\`) || hasControl(reference) {
				raise(structureError("include 必须引用附加文件中的相对路径，不能使用绝对路径或环境变量", name,
					[]any{"include", jsonx.Int(index)}))
			}
			// OxiDNS resolves each include relative to its declaring document.
			// Parent segments inside the managed root are legitimate.
			p := safeRelativePath(path.Clean(path.Join(path.Dir(name), reference)), "include 路径")
			cycle := p == name
			for _, parent := range parents {
				if parent == p {
					cycle = true
				}
			}
			if cycle {
				raise(structureError("OxiDNS include 存在循环引用", name, []any{"include", jsonx.Int(index)}))
			}
			content, ok := assets[p]
			if !ok {
				raise(structureError("OxiDNS include 文件未在附加文件中提供", name, []any{"include", jsonx.Int(index)}))
			}
			if !utf8.Valid(content) {
				raise(structureError("OxiDNS include 文件必须为 UTF-8 YAML", p, nil))
			}
			var included *jsonx.Object
			func() {
				defer func() {
					if r := recover(); r != nil {
						if x, ok := r.(raised); ok {
							if e, ok := x.err.(*Error); ok {
								raise(structureError(e.Message, p, nil))
							}
						}
						panic(r)
					}
				}()
				included = loadNativeYAML(string(content), "include 文件")
			}()
			nested := append(append([]string{}, parents...), name)
			result = append(result, expand(included, p, nested).GetList("plugins")...)
		}
		if name != "config.yaml" {
			globals := jsonx.NewObject()
			for key, value := range document.All() {
				if key != "plugins" {
					globals.Set(key, value)
				}
			}
			includedGlobals = append(includedGlobals, NativeInclude{Name: name, Globals: globals})
		}
		for index := range plugins {
			origins = append(origins, jsonx.Obj("source", name, "index", jsonx.Int(index)))
		}
		combined := make([]any, 0, len(result)+len(plugins))
		combined = append(combined, result...)
		combined = append(combined, plugins...)
		document.Set("plugins", combined)
		return document
	}

	document := expand(loadNativeYAML(get(settings, "native_config", ""), DefaultNativeLabel), "config.yaml", nil)
	metadata := &NativeSources{Plugins: origins, Includes: includedGlobals}
	if sources != nil {
		*sources = *metadata
	}
	if validate {
		issues := validateExpandedDocument(document, metadata)
		for _, issue := range issues {
			if get(issue, "severity", nil) == "error" {
				raise(validationError(issues))
			}
		}
	}
	return document
}

// ResourceReferences is resource_references(document): the read-only local
// resources the document references (plugin files/file/cert/key, nested
// persistent/tls files and ca, API TLS files and the WebUI index), never
// mutable cache/log/database paths.
func ResourceReferences(document *jsonx.Object) (references []string, err error) {
	defer catch(&err)
	return resourceReferences(document), nil
}

func resourceReferences(document any) []string {
	references := []string{}
	add := func(value any) {
		text, ok := value.(string)
		if !ok || text == "" {
			fail("OxiDNS 文件引用必须为非空文本")
		}
		references = append(references, text)
	}
	files := func(args any) {
		if has(args, "files") {
			list, ok := item(args, "files").([]any)
			if !ok {
				fail("OxiDNS 文件引用必须为列表")
			}
			for _, p := range list {
				add(p)
			}
		}
	}
	for _, plugin := range iterate(get(document, "plugins", []any{})) {
		args := orEmpty(get(plugin, "args", nil))
		if !isDict(args) {
			continue
		}
		files(args)
		for _, key := range []string{"file", "cert", "key"} {
			if truthy(get(args, key, nil)) {
				add(item(args, key))
			}
		}
		for _, key := range []string{"persistent", "tls"} {
			nested := orEmpty(get(args, key, nil))
			if isDict(nested) {
				files(nested)
				if truthy(get(nested, "ca", nil)) {
					add(item(nested, "ca"))
				}
			}
		}
	}
	api := orEmpty(get(document, "api", nil))
	var http any = jsonx.NewObject()
	if isDict(api) {
		http = orEmpty(get(api, "http", nil))
	}
	if isDict(http) {
		ssl := orEmpty(get(http, "ssl", nil))
		if isDict(ssl) {
			for _, key := range []string{"cert", "key", "client_ca"} {
				if truthy(get(ssl, key, nil)) {
					add(item(ssl, key))
				}
			}
		}
		webui := orEmpty(get(http, "webui", nil))
		if isDict(webui) && truthy(get(webui, "root", nil)) {
			root, rootIsText := item(webui, "root").(string)
			index, indexIsText := get(webui, "index", "index.html").(string)
			if !rootIsText || !indexIsText {
				fail("OxiDNS WebUI 路径必须为文本")
			}
			add(purePosixPath(root, index))
		}
	}
	return references
}

// orEmpty is `value or {}`.
func orEmpty(value any) any {
	if truthy(value) {
		return value
	}
	return jsonx.NewObject()
}

// iterate is `for x in v`: list items, dict keys, string characters.
func iterate(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case *jsonx.Object:
		if x != nil {
			out := make([]any, 0, x.Len())
			for _, k := range x.Keys() {
				out = append(out, k)
			}
			return out
		}
	case *yamlx.Mapping:
		out := make([]any, 0, x.Len())
		for _, pair := range x.Pairs {
			out = append(out, pair.Key)
		}
		return out
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	}
	raise(pyErr("TypeError", "'"+typeName(v)+"' object is not iterable"))
	return nil
}

// purePosixPath is str(PurePosixPath(parts[0]) / parts[1] / ...): an
// absolute part restarts the path, exactly two leading slashes are kept,
// empty and "." segments and trailing slashes are dropped, "" is ".".
func purePosixPath(parts ...string) string {
	root := ""
	var segments []string
	for _, part := range parts {
		if strings.HasPrefix(part, "/") {
			stripped := strings.TrimLeft(part, "/")
			if len(part)-len(stripped) == 2 {
				root = "//"
			} else {
				root = "/"
			}
			segments = nil
			part = stripped
		}
		for _, segment := range strings.Split(part, "/") {
			if segment != "" && segment != "." {
				segments = append(segments, segment)
			}
		}
	}
	text := root + strings.Join(segments, "/")
	if text == "" {
		return "."
	}
	return text
}

// purePosixParentName is (str(PurePosixPath(p).parent), PurePosixPath(p).name).
func purePosixParentName(p string) (string, string) {
	normalized := purePosixPath(p)
	root := ""
	rest := normalized
	switch {
	case strings.HasPrefix(normalized, "//") && !strings.HasPrefix(normalized, "///"):
		root, rest = "//", normalized[2:]
	case strings.HasPrefix(normalized, "/"):
		root, rest = "/", normalized[1:]
	}
	if rest == "" || rest == "." {
		if root == "" {
			return ".", ""
		}
		return root, ""
	}
	i := strings.LastIndexByte(rest, '/')
	if i < 0 {
		if root == "" {
			return ".", rest
		}
		return root, rest
	}
	return root + rest[:i], rest[i+1:]
}

// RelativeResource is relative_resource(value): nil (None) for an absolute
// or environment-expanded resource (provisioned on the target), else the
// safe relative path.
func RelativeResource(value any) (p *string, err error) {
	defer catch(&err)
	return relativeResource(value), nil
}

func relativeResource(value any) *string {
	if text, ok := value.(string); ok && (strings.HasPrefix(text, "/") || strings.Contains(text, "${")) {
		return nil
	}
	p := safeRelativePath(value, "资源文件路径")
	return &p
}

// ValidateResourceFiles is validate_resource_files(document, paths): every
// relative resource the document references must be among paths, and no
// runtime output (cache dump, query recorder, dynamic domain set) may be one
// of them.
func ValidateResourceFiles(document *jsonx.Object, paths []any) (err error) {
	defer catch(&err)
	validateResourceFiles(document, paths)
	return nil
}

func validateResourceFiles(document any, paths []any) {
	normalized := map[string]bool{}
	for _, p := range paths {
		normalized[safeRelativePath(p, DefaultPathLabel)] = true
	}
	for _, reference := range resourceReferences(document) {
		p := relativeResource(reference)
		if p != nil && !normalized[*p] {
			fail("OxiDNS 配置引用了未打包的资源文件：" + *p)
		}
	}
	// Runtime outputs must never be overwritten or rolled back with config.
	runtime := map[string]string{"cache": "dump_file", "query_recorder": "path", "dynamic_domain_set": "path"}
	for _, plugin := range iterate(get(document, "plugins", []any{})) {
		args := orEmpty(get(plugin, "args", nil))
		if !isDict(args) {
			continue
		}
		kind := get(plugin, "type", nil)
		hashable(kind)
		name, _ := kind.(string)
		key := runtime[name]
		if key != "" && truthy(get(args, key, nil)) {
			p := relativeResource(item(args, key))
			if p != nil && normalized[*p] {
				fail("OxiDNS 运行数据不能与附加文件重合")
			}
		}
	}
}

// hashable raises Python's TypeError for a value used as a dict key that
// cannot be hashed.
func hashable(v any) {
	switch v.(type) {
	case []any:
		raise(pyErr("TypeError", "unhashable type: 'list'"))
	case *jsonx.Object, *yamlx.Mapping:
		raise(pyErr("TypeError", "unhashable type: 'dict'"))
	}
}
