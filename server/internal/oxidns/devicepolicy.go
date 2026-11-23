package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/clientrange"
	"configflow/internal/devicenotes"
	"configflow/internal/netipx"

	"gopkg.in/yaml.v3"
)

// Profile-owned device rules, patched into the native draft without
// regenerating it: two owned plugins and one hook in sequence_main.

const (
	PolicyTag = "cf_device_policy"
	DirectTag = "cf_device_direct"
)

var (
	policyHook   = M("exec", "jump "+PolicyTag)
	directPlugin = M("tag", DirectTag, "type", "sequence", "args", L(
		M("exec", "$forward_local"), M("matches", L("!has_resp"), "exec", "reject 3"), M("exec", "accept")))
)

const clientRangeError = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"

// NormalizeClientRange canonicalizes a client host or CIDR (IPv4-mapped
// peers become IPv4; a host prefix becomes the address).
func NormalizeClientRange(raw *Node) (string, error) {
	if !isStr(raw) {
		return "", fail(clientRangeError)
	}
	value, ok := clientrange.Normalize(raw.Value)
	if !ok {
		return "", fail(clientRangeError)
	}
	return value, nil
}

// NormalizePolicies validates device policy rows ({ip, name, mode}).
func NormalizePolicies(value *Node) (*Node, error) {
	rows, err := parsePolicies(value)
	if err != nil {
		return nil, err
	}
	return policyRowsNode(rows), nil
}

// parsePolicies validates device policy rows into typed rows.
func parsePolicies(value *Node) ([]devicenotes.Policy, error) {
	rows, err := devicenotes.ParsePolicies(ToJSON(value))
	if err != nil {
		return nil, fail(err.Error())
	}
	return rows, nil
}

// storedRows are the validated device_policies of settings ([] when absent).
func storedRows(settings *Node) ([]devicenotes.Policy, error) {
	if !has(settings, "device_policies") {
		return []devicenotes.Policy{}, nil
	}
	return parsePolicies(get(settings, "device_policies"))
}

func keyNodes(m *Node) []*Node {
	var out []*Node
	for _, key := range keys(m) {
		out = append(out, S(key))
	}
	return out
}

// Revision is the sha256 of the settings as sorted compact JSON.
func Revision(settings *Node) string {
	sum := sha256.Sum256(canonicalJSON(settings))
	return hex.EncodeToString(sum[:])
}

func policyPlugin(rows []devicenotes.Policy) *Node {
	var direct []string
	cidr := false
	for _, row := range rows {
		cidr = cidr || strings.Contains(row.IP, "/")
		if row.Mode == "direct" {
			direct = append(direct, row.IP)
		}
	}
	if len(direct) == 0 {
		return nil
	}
	if !cidr {
		return M("tag", PolicyTag, "type", "sequence", "args", L(
			M("matches", L("client_ip "+strings.Join(direct, " ")), "exec", "goto "+DirectTag), M("exec", "return")))
	}
	// More specific ranges override broader ones.
	sorted := slices.Clone(rows)
	key := func(row devicenotes.Policy) netip.Prefix {
		p, _ := netipx.ParseNetwork(row.IP, true)
		return p
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := key(sorted[i]), key(sorted[j])
		if a.Bits() != b.Bits() {
			return a.Bits() > b.Bits()
		}
		if a.Addr().Is4() != b.Addr().Is4() {
			return a.Addr().Is4()
		}
		return a.Addr().Less(b.Addr())
	})
	args := L()
	for _, row := range sorted {
		exec := "return"
		if row.Mode == "direct" {
			exec = "goto " + DirectTag
		}
		args.Content = append(args.Content, M("matches", L("client_ip "+row.IP), "exec", exec))
	}
	args.Content = append(args.Content, M("exec", "return"))
	return M("tag", PolicyTag, "type", "sequence", "args", args)
}

func refuse(reason string, fix ...string) error {
	f := "请先在 OxiDNS 完整参数中调整"
	if len(fix) > 0 {
		f = fix[0]
	}
	return fail(reason + "，无法安全插入设备策略；" + f + "。")
}

// valueText formats resource identities and labels with native scalar values.
func displayValue(n *Node) string {
	if n == nil {
		return "<nil>"
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return string(ToJSON(n))
}

// checkMain: the hook needs a recognizable sequence_main and a local upstream.
func checkMain(document *Node, plugins map[string]*Node) error {
	if externalIncludes(document) {
		return refuse("配置用 include 引入了其他文件，看不到完整的处理链")
	}
	if list := items(get(document, "plugins")); len(plugins) == 0 && len(list) > 0 {
		seen := map[string]bool{}
		for index, item := range list {
			tag, ok := tagOf(item)
			if !ok {
				return refuse(fmt.Sprintf("第 %d 个插件没有 tag", index+1))
			}
			if seen[tag] {
				return refuse("插件 tag「" + tag + "」重复")
			}
			seen[tag] = true
		}
	}
	main := plugins["sequence_main"]
	if main == nil {
		return refuse("找不到主处理链 sequence_main")
	}
	if !equal(get(main, "type"), S("sequence")) {
		if isNone(get(main, "type")) {
			return refuse(messageNullMainType)
		}
		return refuse("主处理链 sequence_main 的类型是 " + displayValue(get(main, "type")) + "，不是 sequence")
	}
	args := get(main, "args")
	if !isSeq(args) || len(args.Content) == 0 {
		return refuse("主处理链 sequence_main 没有处理步骤")
	}
	for index, item := range args.Content {
		if !isStr(get(item, "exec")) || !isMap(item) {
			return refuse(fmt.Sprintf("主处理链 sequence_main 第 %d 步没有 exec 动作", index+1))
		}
	}
	last := args.Content[len(args.Content)-1]
	if !equal(last, M("exec", "goto china_dns")) && !equal(last, M("exec", "goto proxy_dns_seq")) {
		text := str(get(last, "exec"))
		if has(last, "matches") {
			text += "（带匹配条件）"
		} else if mapSize(last) > 1 {
			text += "（带其他字段）"
		}
		return refuse("主处理链 sequence_main 的最后一步是「" + text + "」，设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq")
	}
	forward := plugins["forward_local"]
	switch {
	case forward == nil:
		return refuse("找不到本地上游 forward_local，直连设备没有可用的上游")
	case !equal(get(forward, "type"), S("forward")):
		if isNone(get(forward, "type")) {
			return refuse(messageNullForwardType)
		}
		return refuse("本地上游 forward_local 的类型是 " + displayValue(get(forward, "type")) + "，不是 forward")
	case !isMap(get(forward, "args")) || !(len(items(get(get(forward, "args"), "upstreams"))) > 0):
		return refuse("本地上游 forward_local 没有配置上游地址")
	}
	return nil
}

// listenPort is the listening port, 0 when the core resolves it later.
func listenPort(listen string) int {
	if strings.Contains(listen, "${") {
		return 0
	}
	text := strings.TrimSpace(listen)
	port := text[strings.LastIndexByte(text, ':')+1:]
	n, err := strconv.Atoi(port)
	if err != nil || strings.Trim(port, "0123456789") != "" || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// serverEntries are the executors a DNS server hands queries to.
func serverEntries(plugin *Node) []string {
	args := get(plugin, "args")
	if !isMap(args) {
		return nil
	}
	values := []*Node{get(args, "entry")}
	if equal(get(plugin, "type"), S("http_server")) {
		list := get(args, "entries")
		if !isSeq(list) || len(list.Content) == 0 {
			return nil
		}
		values = nil
		for _, item := range list.Content {
			if !isMap(item) {
				return nil
			}
			values = append(values, get(item, "exec"))
		}
	}
	var out []string
	for _, v := range values {
		if !isStr(v) || v.Value == "" {
			return nil
		}
		out = append(out, v.Value)
	}
	return out
}

// reach is the set of plugins a chain can run from its entries, or the
// reason it cannot be followed.
func reach(plugins map[string]*Node, order []string, start []string) (map[string]bool, string) {
	seen := map[string]bool{}
	queue := append([]string{}, start...)
	for len(queue) > 0 {
		tag := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[tag] {
			continue
		}
		if strings.Contains(tag, "${") {
			return seen, "含未展开的环境变量"
		}
		if plugins[tag] == nil {
			return seen, "用到的插件 " + tag + " 不存在"
		}
		seen[tag] = true
		if tag == "sequence_main" {
			continue
		}
		for _, edge := range ReferenceLocations(M("plugins", L(plugins[tag])), order, true) {
			if edge.Tag == "" {
				return seen, "含未展开的环境变量"
			}
			queue = append(queue, edge.Tag)
		}
	}
	return seen, ""
}

// EntryContext is what ConfigFlow knows about the profile's node-test DNS
// listeners (no I/O): the queried addresses and the profile's OxiDNS hosts.
type EntryContext struct {
	Addresses map[[2]string]bool
	Hosts     map[string]bool
}

// NewEntryContext reads the node-test DNS addresses and OxiDNS Agent hosts.
func NewEntryContext(p *Profile) EntryContext {
	ctx := EntryContext{Addresses: map[[2]string]bool{}, Hosts: map[string]bool{}}
	if p == nil {
		return ctx
	}
	system := p.source.SystemConfig
	values := slices.Clone(system.NodeTestDNS)
	if saved := system.NodeTestDNSConfig; saved != nil && saved.Mode == "custom" {
		values = append(values, saved.Servers...)
	}
	for _, value := range values {
		if strings.Contains(value, "://") {
			continue
		}
		host, port, separator := strings.Cut(strings.TrimSpace(value), ":")
		ip, ok := netipx.ParseAddr4(host)
		if !ok || strings.Contains(host, "%") {
			continue
		}
		if !separator {
			port = "53"
		}
		ctx.Addresses[[2]string{ip.String(), port}] = true
	}
	for _, agent := range p.agents() {
		agentProfile := agent.ProfileID
		if agentProfile == "" {
			agentProfile = "default"
		}
		if agent.ServiceType == "oxidns" && agentProfile == p.profileID() && agent.Host != "" {
			ctx.Hosts[agent.Host] = true
		}
	}
	return ctx
}

type server struct {
	plugin  *Node
	entries []string
}

// legacyNodeTest: a hand-made listener that node tests currently query, on
// a chain of its own (it cannot touch client traffic).
func legacyNodeTest(document *Node, plugins map[string]*Node, servers []server, reached map[string]bool, listen string, ctx EntryContext) bool {
	text := strings.TrimSpace(listen)
	i := strings.LastIndexByte(text, ':')
	host, port := strings.Trim(text[:max(i, 0)], "[]"), text[i+1:]
	matched := false
	for address := range ctx.Addresses {
		if port == address[1] && (host == address[0] || (host == "" || host == "0.0.0.0" || host == "::") && ctx.Hosts[address[0]]) {
			matched = true
		}
	}
	if !matched {
		return false
	}
	entries, names := map[string]bool{}, map[string]bool{}
	var entry string
	for _, s := range servers {
		for _, e := range s.entries {
			entries[e], entry = true, e
		}
		if !isOneOf(get(s.plugin, "type"), "udp_server", "tcp_server") {
			return false
		}
		names[str(get(s.plugin, "tag"))] = true
	}
	if len(entries) != 1 || !equal(get(plugins[entry], "type"), S("sequence")) {
		return false
	}
	for tag := range reached {
		if tag != entry && isOneOf(get(plugins[tag], "type"), "sequence", "fallback") {
			return false
		}
	}
	owners := func(tag string) map[string]bool {
		out := map[string]bool{}
		list := items(get(document, "plugins"))
		for _, edge := range ReferenceLocations(document, []string{tag}, false) {
			owner := "\x00"
			if len(edge.Path) > 1 && edge.Path[0] == (Elem{Key: "plugins"}) && edge.Path[1].IsIndex && edge.Path[1].Index < len(list) {
				owner = str(get(list[edge.Path[1].Index], "tag"))
			}
			out[owner] = true
		}
		return out
	}
	for owner := range owners(entry) {
		if !names[owner] {
			return false
		}
	}
	forwards := 0
	for tag := range reached {
		if !equal(get(plugins[tag], "type"), S("forward")) {
			continue
		}
		forwards++
		for owner := range owners(tag) {
			if owner != entry {
				return false
			}
		}
	}
	return forwards > 0
}

// EntryReport lists listeners that bypass sequence_main.
type EntryReport struct {
	Side     []SideEntry `json:"side_entries"`
	NodeTest []SideEntry `json:"node_test_entries"`
}

// SideEntry is a listener group with its own chain.
type SideEntry struct {
	Tags   []string `json:"tags"`
	Listen string   `json:"listen"`
	Entry  string   `json:"entry"`
	Kind   string   `json:"kind,omitempty"`
}

// InspectEntries checks every DNS listener: client DNS ports must enter
// sequence_main; other listeners on their own chain are reported.
func InspectEntries(document *Node, plugins map[string]*Node, order []string, p *Profile) (EntryReport, error) {
	report := EntryReport{Side: []SideEntry{}, NodeTest: []SideEntry{}}
	var servers []*Node
	for _, tag := range order {
		if kind := get(plugins[tag], "type"); kind != nil && kind.Kind == yaml.ScalarNode && strings.HasSuffix(kind.Value, "_server") {
			servers = append(servers, plugins[tag])
		}
	}
	if len(servers) == 0 {
		return report, refuse("没有任何 DNS 监听入口（udp_server / tcp_server 等）")
	}
	type group struct {
		listen  string
		entries []string
		members []server
	}
	var groups []*group
	mainPorts, hasMain := map[int]bool{53: true}, false
	for _, plugin := range servers {
		listen := str(get(get(plugin, "args"), "listen"))
		if strings.TrimSpace(listen) == "" {
			listen = "未填写监听地址"
		}
		entries := serverEntries(plugin)
		if entries == nil {
			return report, refuse("监听入口 " + str(get(plugin, "tag")) + "（" + listen + "） 的 entry 无法读取")
		}
		allMain := true
		for _, e := range entries {
			allMain = allMain && e == "sequence_main"
		}
		if allMain {
			hasMain = true
			if port := listenPort(listen); port != 0 {
				mainPorts[port] = true
			}
			continue
		}
		key := strings.Join(entries, "\x00")
		var found *group
		for _, g := range groups {
			if g.listen == listen && strings.Join(g.entries, "\x00") == key {
				found = g
			}
		}
		if found == nil {
			found = &group{listen: listen, entries: entries}
			groups = append(groups, found)
		}
		found.members = append(found.members, server{plugin, entries})
	}
	if !hasMain {
		return report, refuse("没有任何监听入口进入主处理链 sequence_main")
	}
	ctx := EntryContext{}
	if len(groups) > 0 {
		ctx = NewEntryContext(p)
	}
	for _, g := range groups {
		var tags, unique []string
		for _, m := range g.members {
			tags = append(tags, str(get(m.plugin, "tag")))
		}
		for _, e := range g.entries {
			if !containsString(unique, e) {
				unique = append(unique, e)
			}
		}
		item := SideEntry{Tags: tags, Listen: g.listen, Entry: strings.Join(unique, "、")}
		name := "监听入口 " + strings.Join(tags, "、") + "（" + g.listen + "）"
		if port := listenPort(g.listen); port == 0 || mainPorts[port] {
			text := "无法确认监听端口"
			if port != 0 {
				text = "监听客户端 DNS 端口 " + strconv.Itoa(port)
			}
			return report, refuse(name+text+"，但 entry 是 "+item.Entry+"，没有进入主处理链 sequence_main（经这里查询的客户端不受设备策略控制）",
				"请在 OxiDNS 完整参数中把它的 entry 改为 sequence_main，或让它改用其他端口")
		}
		reached, problem := reach(plugins, order, g.entries)
		if problem != "" {
			return report, refuse(name + " 的处理链" + problem + "，无法确认它是否经过主处理链")
		}
		if reached["sequence_main"] {
			return report, refuse(name+" 的处理链 "+item.Entry+" 会间接调用主处理链 sequence_main，看不出设备策略是否生效",
				"请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链")
		}
		if legacyNodeTest(document, plugins, g.members, reached, g.listen, ctx) {
			item.Kind = "legacy"
			report.NodeTest = append(report.NodeTest, item)
		} else {
			report.Side = append(report.Side, item)
		}
	}
	return report, nil
}

// orderedPlugins is _plugins with the tag order.
func orderedPlugins(document *Node) (map[string]*Node, []string) {
	plugins := pluginMap(document)
	var order []string
	if len(plugins) > 0 {
		for _, item := range items(get(document, "plugins")) {
			order = append(order, str(get(item, "tag")))
		}
	}
	return plugins, order
}

func patchPolicies(document *Node, previous, rows []devicenotes.Policy, p *Profile) (*Node, EntryReport, error) {
	document = clone(document)
	plugins, order := orderedPlugins(document)
	if err := checkMain(document, plugins); err != nil {
		return nil, EntryReport{}, err
	}
	report, err := InspectEntries(document, plugins, order, p)
	if err != nil {
		return nil, report, err
	}
	main := get(plugins["sequence_main"], "args")
	old := policyPlugin(previous)
	var hooks []int
	for i, item := range main.Content {
		if equal(item, policyHook) {
			hooks = append(hooks, i)
		}
	}
	policy, direct := plugins[PolicyTag], plugins[DirectTag]
	if old != nil {
		if policy == nil || direct == nil || !equal(policy, old) || !equal(direct, directPlugin) || len(hooks) != 1 {
			return nil, report, fail("设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。")
		}
		main.Content = append(main.Content[:hooks[0]:hooks[0]], main.Content[hooks[0]+1:]...)
		if hooks[0] != prefixEnd(main.Content) {
			return nil, report, fail("设备策略的位置已被修改，请先恢复到分流规则之前。")
		}
	} else if policy != nil || direct != nil || len(hooks) > 0 {
		return nil, report, fail("完整参数中存在同名设备策略插件，请先处理命名冲突。")
	}
	remaining := filter(items(get(document, "plugins")), func(item *Node) bool {
		return !isOneOf(get(item, "tag"), PolicyTag, DirectTag)
	})
	if text := string(ToJSON(seqOf(remaining))); strings.Contains(text, PolicyTag) || strings.Contains(text, DirectTag) {
		return nil, report, fail("设备策略插件存在额外引用，请先在完整参数中处理。")
	}
	if next := policyPlugin(rows); next != nil {
		index := prefixEnd(main.Content)
		main.Content = append(main.Content[:index:index], append([]*Node{clone(policyHook)}, main.Content[index:]...)...)
		remaining = append(remaining, next, clone(directPlugin))
	}
	set(document, "plugins", seqOf(remaining))
	return document, report, nil
}

// PatchDocument owns exactly two plugins and one hook in sequence_main;
// altered or ambiguous chains are refused. previous/rows are policy rows.
func PatchDocument(document *Node, previous, rows []devicenotes.Policy, p *Profile) (*Node, error) {
	doc, _, err := patchPolicies(document, previous, rows, p)
	return doc, err
}

// PolicyView is read_policies' answer; Policies are the rows as policy objects.
type PolicyView struct {
	Policies    *Node       `json:"-"`
	Editable    bool        `json:"editable"`
	Reason      string      `json:"reason"`
	SideEntries []SideEntry `json:"side_entries"`
}

// ReadPolicies returns the rows (names from device notes) and whether the
// draft can take them.
func ReadPolicies(settings *Node, p *Profile) (PolicyView, error) {
	names, err := StoredNames(p)
	if err != nil {
		return PolicyView{}, err
	}
	rows, err := storedRows(settings)
	if err != nil {
		return PolicyView{}, err
	}
	for i, row := range rows {
		if name, ok := names[row.IP]; ok {
			rows[i].Name = name
		}
	}
	view := PolicyView{Policies: policyRowsNode(rows), SideEntries: []SideEntry{}}
	current, err := PrepareEditor(settings, p, "")
	if err == nil {
		var doc *Node
		if doc, err = loadNative(get(current, "native_config"), NativeLabel); err == nil {
			var report EntryReport
			if _, report, err = patchPolicies(doc, rows, rows, p); err == nil {
				view.Editable, view.SideEntries = true, report.Side
				return view, nil
			}
		}
	}
	if message, ok := IsSettingsError(err); ok {
		view.Reason = message
		return view, nil
	}
	return PolicyView{}, err
}

// ApplyPolicies stores new rows and patches the native draft.
func ApplyPolicies(settings *Node, p *Profile, value *Node, baseURL string) (*Node, error) {
	rows, err := parsePolicies(value)
	if err != nil {
		return nil, err
	}
	current, err := PrepareEditor(settings, p, baseURL)
	if err != nil {
		return nil, err
	}
	previous, err := storedRows(current)
	if err != nil {
		return nil, err
	}
	document, err := loadNative(get(current, "native_config"), NativeLabel)
	if err != nil {
		return nil, err
	}
	if document, err = PatchDocument(document, previous, rows, p); err != nil {
		return nil, err
	}
	if err := setNativeConfig(current, document); err != nil {
		return nil, err
	}
	set(current, "device_policies", policyRowsNode(rows))
	if template := get(current, "template_settings"); isMap(template) {
		set(template, "device_policies", policyRowsNode(rows))
	}
	return current, nil
}
