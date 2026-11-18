package oxidns

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Limits of native documents and attachments.
const (
	MaxNativeBytes          = 2 << 20
	MaxAdditionalFiles      = 256
	MaxAdditionalFileBytes  = 16 << 20
	MaxAdditionalTotalBytes = 64 << 20
)

// TagPattern is a valid plugin tag (1–64 characters is checked separately).
var TagPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*$`)

// validTag: a plugin tag the core accepts.
func validTag(tag string) bool {
	return len(tag) > 0 && len(tag) <= 64 && TagPattern.MatchString(tag) &&
		!strings.HasPrefix(tag, "qs.exec.") && !strings.HasPrefix(tag, "qs.match.") && !strings.HasPrefix(tag, "qs.cron.")
}

const tagUnsupported = "!unsupported"

// LoadNativeYAML parses native OxiDNS YAML: YAML 1.2 scalars (no yes/no,
// no timestamps, 0x/0o/0b ints), safe string keys, no duplicates, plain
// objects/arrays/finite scalars only, depth ≤ 32, ≤ 50000 nodes, mapping at
// the top. label defaults to "完整配置".
func LoadNativeYAML(text string, label string) (*Node, error) { return loadNative(S(text), label) }

func loadNative(text *Node, label string) (*Node, error) {
	if !isStr(text) || len(text.Value) > MaxNativeBytes {
		return nil, fail(fmt.Sprintf("OxiDNS %s必须为不超过 2 MiB 的 YAML 文本", label))
	}
	syntax := fail("OxiDNS " + label + " YAML 语法无效")
	value, err := parseYAML(text.Value, syntax)
	if err != nil {
		return nil, err
	}
	count := 0
	active := map[*Node]bool{}
	var visit func(n *Node, depth int) error
	visit = func(n *Node, depth int) error {
		n = yamlx.Resolve(n)
		count++
		if count > 50000 || depth > 32 {
			return fail("OxiDNS YAML 超出结构大小限制")
		}
		switch {
		case n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode:
			if active[n] {
				return fail("OxiDNS YAML 不支持循环别名")
			}
			active[n] = true
			for i, child := range n.Content {
				if n.Kind == yaml.MappingNode && i%2 == 0 {
					continue
				}
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
			delete(active, n)
		case n.Tag == tagUnsupported:
			return fail("OxiDNS YAML 只支持普通对象、数组和标量")
		case n.Tag == tagFloat:
			if f, _ := strconv.ParseFloat(n.Value, 64); math.IsInf(f, 0) || math.IsNaN(f) {
				return fail("OxiDNS YAML 数字必须为有限值")
			}
		}
		return nil
	}
	if err := visit(value, 0); err != nil {
		return nil, err
	}
	if !isMap(value) {
		return nil, fail("OxiDNS " + label + "顶层必须为对象")
	}
	return value, nil
}

// parseYAML reads native YAML with the shared D1/alias policy and validates
// the OxiDNS document schema. Scalar interpretation belongs to yamlx alone.
func parseYAML(text string, syntax error) (*Node, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		if errors.Is(err, yamlx.ErrSyntax) {
			return nil, syntax
		}
		return nil, yamlReaderFailure(err)
	}
	var validate func(*Node) error
	validate = func(node *Node) error {
		node = yamlx.Resolve(node)
		if node.Kind == yaml.MappingNode {
			for key, value := range yamlx.AllPairs(node) {
				key = yamlx.Resolve(key)
				if !isStr(key) || key.Value == "__proto__" || key.Value == "prototype" || key.Value == "constructor" {
					return fail(messageSafeFieldNames)
				}
				if err := validate(value); err != nil {
					return err
				}
			}
			return nil
		}
		if node.Kind == yaml.SequenceNode {
			for _, value := range node.Content {
				if err := validate(value); err != nil {
					return err
				}
			}
			return nil
		}
		switch node.ShortTag() {
		case tagStr, tagBool, tagNull, tagInt, "!!timestamp", "!!binary":
			return nil
		case tagFloat:
			var value float64
			if err := yamlx.Decode(node, &value); err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return fail(messageFiniteNumbers)
			}
			return nil
		default:
			return errUnsupportedYAML
		}
	}
	if err := validate(root); err != nil {
		return nil, err
	}
	yamlx.MaterializeAliases(root)
	return root, nil
}

// File is a deployment file: managed attachments are Native.
type File struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Native  bool   `json:"native,omitempty"`
}

// ManagedFiles is managed_files: validated attachment metadata and bytes.
func ManagedFiles(value *Node) ([]File, error) {
	if !isSeq(value) || len(value.Content) > MaxAdditionalFiles {
		return nil, fail("附加文件必须为数组，最多 256 个文件")
	}
	result := []File{}
	seen := map[string]bool{}
	total := 0
	for _, item := range value.Content {
		if !isMap(item) {
			return nil, fail("附加文件条目必须为对象")
		}
		p, err := SafeRelativePath(get(item, "path"), "附加文件路径")
		if err != nil {
			return nil, err
		}
		if p == "config.yaml" || p == "manifest.json" || p == "state" || strings.HasPrefix(p, "state/") || strings.HasPrefix(p, ".configflow") {
			return nil, fail("附加文件不能覆盖主配置、部署元数据或 state/ 运行数据")
		}
		if seen[p] {
			return nil, fail("附加文件路径重复")
		}
		for previous := range seen {
			if strings.HasPrefix(p, previous+"/") || strings.HasPrefix(previous, p+"/") {
				return nil, fail("附加文件路径存在文件与目录冲突")
			}
		}
		seen[p] = true
		content, encoding := get(item, "content"), "text"
		if has(item, "encoding") {
			encoding = "\x00"
			if e := get(item, "encoding"); isStr(e) {
				encoding = e.Value
			}
		}
		if !isStr(content) || (encoding != "text" && encoding != "base64") {
			return nil, fail("附加文件内容必须为文本，编码为 text 或 base64")
		}
		if utf8.RuneCountInString(content.Value) > MaxAdditionalFileBytes*2 {
			return nil, fail("单个附加文件不能超过 16 MiB")
		}
		data := []byte(content.Value)
		if encoding == "base64" {
			if data, err = decodeBase64(content.Value); err != nil {
				return nil, fail("附加文件 Base64 内容无效")
			}
		}
		if len(data) > MaxAdditionalFileBytes {
			return nil, fail("单个附加文件不能超过 16 MiB")
		}
		if total += len(data); total > MaxAdditionalTotalBytes {
			return nil, fail("附加文件总大小不能超过 64 MiB")
		}
		result = append(result, File{Path: p, Content: data, Native: true})
	}
	return result, nil
}

// Origin is where a plugin of an expanded document was declared.
type Origin struct {
	Source string `json:"source"`
	Index  int    `json:"index"`
}

// Included are the globals of one include file (ignored by the core, but
// type-checked).
type Included struct {
	Name    string
	Globals *Node
}

// Sources describes an expanded document: origin per plugin, include globals.
type Sources struct {
	Plugins  []Origin
	Includes []Included
}

func structureError(message, source string, at Path) error {
	return &SettingsError{Msg: message, Issues: []Issue{{Path: at, Source: source, Message: message, Severity: "error", Code: "structure"}}}
}

// NativeDocument is native_document: the native YAML of settings with its
// includes expanded like the core (included plugins first, their globals
// ignored). validate runs the full validation. sources, when not nil,
// receives the origins.
func NativeDocument(settings *Node, validate bool, sources *Sources) (*Node, error) {
	files := get(settings, "additional_files")
	if !has(settings, "additional_files") {
		files = L()
	}
	managed, err := ManagedFiles(files)
	if err != nil {
		return nil, err
	}
	assets := map[string][]byte{}
	for _, f := range managed {
		assets[f.Path] = f.Content
	}
	meta := &Sources{Plugins: []Origin{}, Includes: []Included{}}
	count := 0
	var expand func(document *Node, name string, parents []string) error
	expand = func(document *Node, name string, parents []string) error {
		count++
		if len(parents) > 8 || count > 512 {
			return structureError("OxiDNS include 超过 8 层或文件数量限制", name, P("include"))
		}
		includes := del(document, "include")
		if includes == nil {
			includes = L()
		}
		if !allStrings(includes) {
			return structureError("OxiDNS include 必须为文件路径数组", name, P("include"))
		}
		plugins := get(document, "plugins")
		if !has(document, "plugins") {
			plugins = L()
		}
		if !isSeq(plugins) {
			return structureError("OxiDNS plugins 必须为数组", name, P("plugins"))
		}
		var result []*Node
		for index, ref := range includes.Content {
			reference := ref.Value
			if reference == "" || strings.HasPrefix(reference, "/") || strings.Contains(reference, "${") ||
				strings.ContainsRune(reference, '\\') || hasControl(reference) {
				return structureError("include 必须引用附加文件中的相对路径，不能使用绝对路径或环境变量", name, P("include", index))
			}
			p, err := safePath(path.Clean(path.Join(path.Dir(name), reference)), "include 路径")
			if err != nil {
				return err
			}
			if p == name || containsString(parents, p) {
				return structureError("OxiDNS include 存在循环引用", name, P("include", index))
			}
			data, ok := assets[p]
			if !ok {
				return structureError("OxiDNS include 文件未在附加文件中提供", name, P("include", index))
			}
			if !utf8.Valid(data) {
				return structureError("OxiDNS include 文件必须为 UTF-8 YAML", p, nil)
			}
			included, err := LoadNativeYAML(string(data), "include 文件")
			if err != nil {
				return yamlReaderContext(err, p, nil)
			}
			if err := expand(included, p, append(append([]string{}, parents...), name)); err != nil {
				return err
			}
			result = append(result, get(included, "plugins").Content...)
		}
		if name != "config.yaml" {
			globals := M()
			pairs(document, func(key string, value *Node) bool {
				if key != "plugins" {
					set(globals, key, value)
				}
				return true
			})
			meta.Includes = append(meta.Includes, Included{name, globals})
		}
		for i := range plugins.Content {
			meta.Plugins = append(meta.Plugins, Origin{name, i})
		}
		set(document, "plugins", seqOf(append(result, plugins.Content...)))
		return nil
	}
	text := get(settings, "native_config")
	if !has(settings, "native_config") {
		text = S("")
	}
	document, err := loadNative(text, NativeLabel)
	if err != nil {
		return nil, err
	}
	if err := expand(document, "config.yaml", nil); err != nil {
		return nil, err
	}
	if sources != nil {
		*sources = *meta
	}
	if validate {
		if issues := ValidateExpandedDocument(document, meta); hasError(issues) {
			return nil, &ValidationError{issues}
		}
	}
	return document, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// pluginList iterates document.get('plugins', []) like Python: a missing key
// is empty, a non-list that Python cannot iterate as plugins is malformed.
func pluginList(document *Node) ([]*Node, error) {
	plugins := get(document, "plugins")
	switch {
	case !has(document, "plugins"):
		return nil, nil
	case isSeq(plugins):
		for _, p := range plugins.Content {
			if !isMap(p) {
				return nil, errMalformed
			}
		}
		return plugins.Content, nil
	case (isMap(plugins) || isStr(plugins)) && (len(plugins.Content) == 0 && plugins.Value == ""):
		return nil, nil
	}
	return nil, errMalformed
}

// ResourceReferences lists read-only local resources the document reads.
func ResourceReferences(document *Node) ([]string, error) {
	references := []string{}
	add := func(value *Node) error {
		if !isStr(value) || value.Value == "" {
			return fail("OxiDNS 文件引用必须为非空文本")
		}
		references = append(references, value.Value)
		return nil
	}
	files := func(args *Node) error {
		if !has(args, "files") {
			return nil
		}
		list := get(args, "files")
		if !isSeq(list) {
			return fail("OxiDNS 文件引用必须为列表")
		}
		for _, p := range list.Content {
			if err := add(p); err != nil {
				return err
			}
		}
		return nil
	}
	plugins, err := pluginList(document)
	if err != nil {
		return nil, err
	}
	for _, plugin := range plugins {
		args := get(plugin, "args")
		if !(!isNone(args)) {
			args = M()
		}
		if !isMap(args) {
			continue
		}
		if err := files(args); err != nil {
			return nil, err
		}
		for _, key := range []string{"file", "cert", "key"} {
			if str(get(args, key)) != "" {
				if err := add(get(args, key)); err != nil {
					return nil, err
				}
			}
		}
		for _, key := range []string{"persistent", "tls"} {
			nested := get(args, key)
			if !isMap(nested) {
				continue
			}
			if err := files(nested); err != nil {
				return nil, err
			}
			if str(get(nested, "ca")) != "" {
				if err := add(get(nested, "ca")); err != nil {
					return nil, err
				}
			}
		}
	}
	api := get(document, "api")
	if !(isMap(api) && mapSize(api) > 0) {
		api = M()
	}
	var http *Node
	if isMap(api) {
		if http = get(api, "http"); !(isMap(http) && mapSize(http) > 0) {
			http = M()
		}
	} else if isStr(api) || isSeq(api) || isTag(api, tagInt) || isTag(api, tagFloat) || isBool(api) {
		return references, nil
	}
	if !isMap(http) {
		return references, nil
	}
	if ssl := get(http, "ssl"); isMap(ssl) {
		for _, key := range []string{"cert", "key", "client_ca"} {
			if str(get(ssl, key)) != "" {
				if err := add(get(ssl, key)); err != nil {
					return nil, err
				}
			}
		}
	}
	if webui := get(http, "webui"); isMap(webui) && resourcePresent(get(webui, "root")) {
		root, index := get(webui, "root"), get(webui, "index")
		if !has(webui, "index") {
			index = S("index.html")
		}
		if !isStr(root) || !isStr(index) {
			return nil, fail("OxiDNS WebUI 路径必须为文本")
		}
		if err := add(S(purePosixJoin(root.Value, index.Value))); err != nil {
			return nil, err
		}
	}
	return references, nil
}

// purePosixJoin is str(PurePosixPath(a) / b).
func purePosixJoin(a, b string) string {
	if strings.HasPrefix(b, "/") {
		a = ""
	}
	full := a + "/" + b
	if a == "" {
		full = b
	}
	var parts []string
	for _, part := range strings.Split(full, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	prefix := ""
	if strings.HasPrefix(full, "//") && !strings.HasPrefix(full, "///") {
		prefix = "//"
	} else if strings.HasPrefix(full, "/") {
		prefix = "/"
	}
	if len(parts) == 0 {
		if prefix != "" {
			return prefix
		}
		return "."
	}
	return prefix + strings.Join(parts, "/")
}

// RelativeResource is relative_resource: the managed path of a resource
// reference; ok false for absolute or ${ENV} references (provisioned on the
// target).
func RelativeResource(value *Node) (string, bool, error) {
	if isStr(value) && (strings.HasPrefix(value.Value, "/") || strings.Contains(value.Value, "${")) {
		return "", false, nil
	}
	p, err := SafeRelativePath(value, "资源文件路径")
	return p, err == nil, err
}

func relativeResource(value string) (string, bool, error) { return RelativeResource(S(value)) }

// referencedPaths is {relative_resource(p) for p in resource_references(doc)}
// ("" stands for Python's None).
func referencedPaths(document *Node) (map[string]bool, error) {
	references, err := ResourceReferences(document)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, ref := range references {
		p, _, err := relativeResource(ref)
		if err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, nil
}

// ValidateResourceFiles checks every relative resource is shipped and that
// runtime outputs are not attachments.
func ValidateResourceFiles(document *Node, paths []string) error {
	normalized := map[string]bool{}
	for _, p := range paths {
		n, err := safePath(p, PathLabel)
		if err != nil {
			return err
		}
		normalized[n] = true
	}
	references, err := ResourceReferences(document)
	if err != nil {
		return err
	}
	for _, ref := range references {
		p, ok, err := relativeResource(ref)
		if err != nil {
			return err
		}
		if ok && !normalized[p] {
			return fail("OxiDNS 配置引用了未打包的资源文件：" + p)
		}
	}
	plugins, _ := pluginList(document)
	for _, plugin := range plugins {
		args := get(plugin, "args")
		if !(!isNone(args)) {
			args = M()
		}
		if !isMap(args) {
			continue
		}
		kind := get(plugin, "type")
		if isMap(kind) || isSeq(kind) {
			return errMalformed
		}
		key := map[string]string{"cache": "dump_file", "query_recorder": "path", "dynamic_domain_set": "path"}[str(kind)]
		if key != "" && resourcePresent(get(args, key)) {
			p, ok, err := RelativeResource(get(args, key))
			if err != nil {
				return err
			}
			if ok && normalized[p] {
				return fail("OxiDNS 运行数据不能与附加文件重合")
			}
		}
	}
	return nil
}

// NativeLabel names the main native configuration in messages; PathLabel is
// safe_relative_path's default label.
const (
	NativeLabel = "完整配置"
	PathLabel   = "文件路径"
)

// decodeBase64 is strict standard base64: only alphabet characters, then the
// padding a partial final quantum needs; '=' after a complete quantum is
// ignored (as the panel always accepted).
func decodeBase64(text string) ([]byte, error) {
	core := strings.TrimRight(text, "=")
	padding := len(text) - len(core)
	if core == "" && padding > 0 {
		return nil, errors.New("invalid base64 padding")
	}
	switch len(core) % 4 {
	case 1:
		return nil, errors.New("invalid base64 length")
	case 2:
		if padding != 2 {
			return nil, errors.New("invalid base64 padding")
		}
	case 3:
		if padding != 1 {
			return nil, errors.New("invalid base64 padding")
		}
	}
	if strings.ContainsAny(core, "=\r\n") {
		return nil, errors.New("invalid base64 data")
	}
	return base64.RawStdEncoding.DecodeString(core)
}

// duplicateAnchor: an anchor name defined twice (PyYAML refuses that; yaml.v3
// lets the later one win).
func duplicateAnchor(n *Node, seen map[string]bool) bool {
	if n == nil || n.Kind == yaml.AliasNode {
		return false
	}
	if n.Anchor != "" {
		if seen[n.Anchor] {
			return true
		}
		seen[n.Anchor] = true
	}
	for _, child := range n.Content {
		if duplicateAnchor(child, seen) {
			return true
		}
	}
	return false
}

func resourcePresent(value *Node) bool { return !isNone(value) && (!isStr(value) || str(value) != "") }
