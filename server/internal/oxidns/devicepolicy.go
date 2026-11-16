package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// The parts of backend/common/oxidns_device_policy.py (module C26) that the
// settings normalizer (C05) calls. C26 adds the rest of the module here.

var (
	clientAddressPattern = regexp.MustCompile(`^[0-9a-fA-F:.]+$`)
	clientPrefixPattern  = regexp.MustCompile(`^[0-9]{1,3}$`)
)

const clientRangeMessage = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"

// NormalizeClientRange is normalize_client_range(raw): a host or CIDR in
// canonical text (IPv4-mapped peers as IPv4), or OxidnsSettingsError.
func NormalizeClientRange(raw any) (text string, err error) {
	defer catch(&err)
	return normalizeClientRange(raw), nil
}

func normalizeClientRange(raw any) string {
	text, ok := raw.(string)
	if !ok {
		fail(clientRangeMessage)
	}
	parts := strings.Split(pystr.Strip(text), "/")
	if len(parts) > 2 || !clientAddressPattern.MatchString(parts[0]) {
		fail(clientRangeMessage)
	}
	address, ok := convert.ParseIPAddress(parts[0])
	if !ok {
		fail(clientRangeMessage)
	}
	prefix := address.BitLen()
	if len(parts) == 2 {
		if !clientPrefixPattern.MatchString(parts[1]) {
			fail(clientRangeMessage)
		}
		prefix, _ = strconv.Atoi(parts[1])
	}
	if prefix > address.BitLen() {
		fail(clientRangeMessage)
	}
	// OxiDNS sees IPv4-mapped peers as the same IPv4 client.
	if address.Is4In6() {
		if prefix < 96 {
			fail(clientRangeMessage)
		}
		address, prefix = address.Unmap(), prefix-96
	}
	network := netip.PrefixFrom(address, prefix).Masked()
	if network.Addr().IsMulticast() && lastAddress(network).IsMulticast() {
		fail(clientRangeMessage)
	}
	if prefix == address.BitLen() {
		if address.IsUnspecified() {
			fail(clientRangeMessage)
		}
		return address.String()
	}
	return network.Addr().String() + "/" + strconv.Itoa(prefix)
}

// lastAddress is network.broadcast_address.
func lastAddress(network netip.Prefix) netip.Addr {
	bytes := network.Addr().AsSlice()
	for bit := network.Bits(); bit < len(bytes)*8; bit++ {
		bytes[bit/8] |= 1 << (7 - bit%8)
	}
	address, _ := netip.AddrFromSlice(bytes)
	return address
}

// NormalizePolicies is normalize_policies(value): the validated device
// policy rows ({ip, name, mode}).
func NormalizePolicies(value any) (rows []any, err error) {
	defer catch(&err)
	return normalizePolicies(value), nil
}

func normalizePolicies(value any) []any {
	list, ok := value.([]any)
	if !ok || len(list) > 512 {
		fail("设备策略必须为列表，最多 512 条策略。")
	}
	result := []any{}
	seen := map[string]bool{}
	for _, raw := range list {
		row, ok := raw.(*jsonx.Object)
		if ok && row != nil {
			for _, key := range row.Keys() {
				if !strIn(key, "ip", "name", "mode") {
					ok = false
				}
			}
		}
		if !ok || row == nil {
			fail("设备策略仅支持 IP / 网段、备注和策略。")
		}
		ip := normalizeClientRange(row.Value("ip"))
		if seen[ip] {
			fail("客户端 IP / 网段重复：" + ip)
		}
		seen[ip] = true
		name, mode := row.GetOr("name", ""), row.Value("mode")
		text, isText := name.(string)
		if !isText || runeLen(text) > 80 || hasControl(text) {
			fail("设备备注最多 80 个字符，不能包含换行或控制字符。")
		}
		if !strIn(mode, "smart", "direct") {
			fail("设备策略必须为智能分流或直连。")
		}
		// Older versions accepted DEL in names; drop it so the row stays editable.
		result = append(result, jsonx.Obj("ip", ip, "name", pystr.Strip(strings.ReplaceAll(text, "\x7f", "")), "mode", mode))
	}
	return result
}

// ---- the rest of oxidns_device_policy.py (C26) ----

// Owned plugin tags and fixed values of the device policy.
const (
	PolicyTag = "cf_device_policy"
	DirectTag = "cf_device_direct"
)

// policyHook is HOOK, directPlugin DIRECT (fresh copies).
func policyHook() *jsonx.Object { return jsonx.Obj("exec", "jump "+PolicyTag) }

func directPlugin() *jsonx.Object {
	return jsonx.Obj("tag", DirectTag, "type", "sequence", "args", []any{
		jsonx.Obj("exec", "$forward_local"), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3"),
		jsonx.Obj("exec", "accept"),
	})
}

// Revision is revision(settings): sha256 hex of the settings as sorted,
// compact, non-ASCII-escaped JSON.
func Revision(settings any) (string, error) {
	data, err := jsonx.Encode(settings, jsonx.Options{SortKeys: true, Compact: true})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// policyPlugin is _policy_plugin(rows) (nil = None).
func policyPlugin(rows []any) *jsonx.Object {
	direct := []string{}
	ranged := false
	for _, row := range rows {
		if item(row, "mode") == "direct" {
			direct = append(direct, str(item(row, "ip")))
		}
	}
	if len(direct) == 0 {
		return nil
	}
	for _, row := range rows {
		if strings.Contains(str(item(row, "ip")), "/") {
			ranged = true
		}
	}
	if ranged {
		// More specific ranges (including individual clients) override broader
		// ones. A smart exception must return to the caller before a broader
		// direct rule.
		type keyed struct {
			row    any
			prefix netip.Prefix
		}
		sortedRows := make([]keyed, 0, len(rows))
		for _, row := range rows {
			text := str(item(row, "ip"))
			prefix, err := netip.ParsePrefix(text)
			if err != nil {
				address, err := netip.ParseAddr(text)
				if err != nil {
					raise(pyErr("ValueError", text+" does not appear to be an IPv4 or IPv6 network"))
				}
				prefix = netip.PrefixFrom(address, address.BitLen())
			}
			sortedRows = append(sortedRows, keyed{row, prefix})
		}
		sort.SliceStable(sortedRows, func(i, j int) bool {
			a, b := sortedRows[i].prefix, sortedRows[j].prefix
			if a.Bits() != b.Bits() {
				return a.Bits() > b.Bits()
			}
			if a.Addr().Is4() != b.Addr().Is4() {
				return a.Addr().Is4()
			}
			return a.Addr().Less(b.Addr())
		})
		args := []any{}
		for _, entry := range sortedRows {
			exec := "return"
			if item(entry.row, "mode") == "direct" {
				exec = "goto " + DirectTag
			}
			args = append(args, jsonx.Obj("matches", []any{"client_ip " + str(item(entry.row, "ip"))}, "exec", exec))
		}
		return jsonx.Obj("tag", PolicyTag, "type", "sequence", "args", append(args, jsonx.Obj("exec", "return")))
	}
	// Preserve the original host-only shape so existing drafts stay editable.
	return jsonx.Obj("tag", PolicyTag, "type", "sequence", "args", []any{
		jsonx.Obj("matches", []any{"client_ip " + strings.Join(direct, " ")}, "exec", "goto "+DirectTag),
		jsonx.Obj("exec", "return"),
	})
}

// refuse is _refuse(reason, fix).
func refuse(reason string, fix ...string) {
	how := "请先在 OxiDNS 完整参数中调整"
	if len(fix) > 0 {
		how = fix[0]
	}
	fail(reason + "，无法安全插入设备策略；" + how + "。")
}

// checkMain is _check_main(document, plugins): the hook is only placed in a
// recognizable sequence_main with a local upstream.
func checkMain(document, plugins *jsonx.Object) {
	if truthy(document.Value("include")) {
		refuse("配置用 include 引入了其他文件，看不到完整的处理链")
	}
	if items, ok := document.Value("plugins").([]any); plugins.Len() == 0 && ok && len(items) > 0 {
		seen := map[string]bool{}
		for index, element := range items {
			var tag any
			if isDict(element) {
				tag = get(element, "tag", nil)
			}
			text, ok := tag.(string)
			if !ok {
				refuse("第 " + itoa(index+1) + " 个插件没有 tag")
			}
			if seen[text] {
				refuse("插件 tag「" + text + "」重复")
			}
			seen[text] = true
		}
	}
	main := pluginAt(plugins, "sequence_main")
	if main == nil {
		refuse("找不到主处理链 sequence_main")
	}
	if main.Value("type") != "sequence" {
		refuse("主处理链 sequence_main 的类型是 " + str(main.Value("type")) + "，不是 sequence")
	}
	args, ok := main.Value("args").([]any)
	if !ok || len(args) == 0 {
		refuse("主处理链 sequence_main 没有处理步骤")
	}
	for index, element := range args {
		obj, ok := element.(*jsonx.Object)
		if !ok || obj == nil || !isStr(obj.Value("exec")) {
			refuse("主处理链 sequence_main 第 " + itoa(index+1) + " 步没有 exec 动作")
		}
	}
	last := args[len(args)-1].(*jsonx.Object)
	if !jsonx.Equal(last, jsonx.Obj("exec", "goto china_dns")) && !jsonx.Equal(last, jsonx.Obj("exec", "goto proxy_dns_seq")) {
		hasMatches, hasExtra := false, false
		for _, key := range last.Keys() {
			if key != "exec" {
				hasExtra = true
				if key == "matches" {
					hasMatches = true
				}
			}
		}
		text := last.Value("exec").(string)
		if hasMatches {
			text += "（带匹配条件）"
		} else if hasExtra {
			text += "（带其他字段）"
		}
		refuse("主处理链 sequence_main 的最后一步是「" + text + "」，" +
			"设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq")
	}
	forward := pluginAt(plugins, "forward_local")
	if forward == nil {
		refuse("找不到本地上游 forward_local，直连设备没有可用的上游")
	}
	if forward.Value("type") != "forward" {
		refuse("本地上游 forward_local 的类型是 " + str(forward.Value("type")) + "，不是 forward")
	}
	if args, ok := forward.Value("args").(*jsonx.Object); !ok || args == nil || !truthy(args.Value("upstreams")) {
		refuse("本地上游 forward_local 没有配置上游地址")
	}
}

// listenPort is _listen_port(listen): the port, ok false when the core
// resolves the address later.
func listenPort(listen any) (int, bool) {
	text, ok := listen.(string)
	if !ok || strings.Contains(text, "${") {
		return 0, false
	}
	trimmed := strings.TrimSpace(text)
	port := trimmed[strings.LastIndex(trimmed, ":")+1:]
	if port == "" || strings.TrimLeft(port, "0123456789") != "" {
		return 0, false
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return 0, false
	}
	return value, true
}

// serverEntries is _server_entries(plugin): the executors a DNS server
// hands queries to, ok false when they cannot be read.
func serverEntries(plugin *jsonx.Object) ([]string, bool) {
	args, ok := plugin.Value("args").(*jsonx.Object)
	if !ok || args == nil {
		return nil, false
	}
	var values []any
	if plugin.Value("type") == "http_server" {
		items, ok := args.Value("entries").([]any)
		if !ok || len(items) == 0 {
			return nil, false
		}
		for _, element := range items {
			if !isDict(element) {
				return nil, false
			}
		}
		for _, element := range items {
			values = append(values, get(element, "exec", nil))
		}
	} else {
		values = []any{args.Value("entry")}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok || text == "" {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

// reach is _reach(document, plugins, start): the plugins a chain can run
// from its entries, and the unresolved reason ("" when complete).
func reach(plugins *jsonx.Object, start []string) (map[string]bool, string) {
	tags := plugins.Keys()
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
		if !plugins.Has(tag) {
			return seen, "用到的插件 " + tag + " 不存在"
		}
		seen[tag] = true
		if tag == "sequence_main" {
			continue // Reaching it is the answer; its own graph is irrelevant here.
		}
		for _, edge := range referenceLocations(jsonx.Obj("plugins", []any{plugins.Value(tag)}), tags, true) {
			next, ok := get(edge, "tag", nil).(string)
			if !ok {
				return seen, "含未展开的环境变量"
			}
			queue = append(queue, next)
		}
	}
	return seen, ""
}

// listenEndpoint is _endpoint(listen): (host without brackets, port).
func listenEndpoint(listen string) (string, string) {
	trimmed := strings.TrimSpace(listen)
	host, port := "", trimmed
	if i := strings.LastIndex(trimmed, ":"); i >= 0 {
		host, port = trimmed[:i], trimmed[i+1:]
	}
	return strings.Trim(host, "[]"), port
}

// EntryContext is entry_context(config): what ConfigFlow itself knows about
// node-test DNS listeners of this profile (no I/O). Present is false for
// Python's {} (config not a dict).
type EntryContext struct {
	Present   bool
	ProfileID any
	Agents    []any
	Addresses map[[2]string]bool // (IPv4, port)
	Hosts     map[string]bool
}

// Object is the context as a JSON-able value: sets as sorted lists
// (addresses as [ip, port] pairs).
func (c EntryContext) Object() *jsonx.Object {
	if !c.Present {
		return jsonx.NewObject()
	}
	addresses := make([][2]string, 0, len(c.Addresses))
	for address := range c.Addresses {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i][0] != addresses[j][0] {
			return addresses[i][0] < addresses[j][0]
		}
		return addresses[i][1] < addresses[j][1]
	})
	pairs := []any{}
	for _, address := range addresses {
		pairs = append(pairs, []any{address[0], address[1]})
	}
	hosts := make([]string, 0, len(c.Hosts))
	for host := range c.Hosts {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	hostList := []any{}
	for _, host := range hosts {
		hostList = append(hostList, host)
	}
	agents := c.Agents
	if agents == nil {
		agents = []any{}
	}
	return jsonx.Obj("profile_id", c.ProfileID, "agents", agents, "addresses", pairs, "hosts", hostList)
}

// NewEntryContext is entry_context(config).
func NewEntryContext(config any) (context EntryContext, err error) {
	defer catch(&err)
	return entryContext(config), nil
}

func entryContext(configValue any) EntryContext {
	config, ok := configValue.(*jsonx.Object)
	if !ok || config == nil {
		return EntryContext{}
	}
	profileID := config.Value("profile_id")
	agents := []any{}
	if value := config.Value("agents"); truthy(value) {
		for _, agent := range pyIter(value) {
			if isDict(agent) {
				agents = append(agents, agent)
			}
		}
	}
	system, ok := config.Value("system_config").(*jsonx.Object)
	if !ok || system == nil {
		system = jsonx.NewObject()
	}
	values := []any{}
	if list, ok := system.Value("node_test_dns").([]any); ok {
		values = append(values, list...)
	}
	if saved, ok := system.Value("node_test_dns_config").(*jsonx.Object); ok && saved != nil && saved.Value("mode") == "custom" {
		if servers, ok := saved.Value("servers").([]any); ok {
			values = append(values, servers...)
		}
	}
	addresses := map[[2]string]bool{}
	for _, value := range values {
		text, ok := value.(string)
		if !ok || strings.Contains(text, "://") {
			continue
		}
		host, port, separator := strings.Cut(strings.TrimSpace(text), ":")
		address, err := netip.ParseAddr(host)
		if err != nil || !address.Is4() {
			continue
		}
		if !separator {
			port = "53"
		}
		addresses[[2]string{address.String(), port}] = true
	}
	hosts := map[string]bool{}
	for _, agent := range agents {
		host, ok := get(agent, "host", nil).(string)
		if get(agent, "service_type", nil) == "oxidns" && jsonx.Equal(get(agent, "profile_id", "default"), profileID) && ok {
			hosts[host] = true
		}
	}
	return EntryContext{Present: true, ProfileID: profileID, Agents: agents, Addresses: addresses, Hosts: hosts}
}

type serverMember struct {
	plugin  *jsonx.Object
	entries []string
}

// legacyNodeTest is _legacy_node_test(document, plugins, servers, reach,
// listen, context): a hand-made listener node tests currently query, on an
// isolated chain.
func legacyNodeTest(document, plugins *jsonx.Object, servers []serverMember, reached map[string]bool, listen string, context EntryContext) bool {
	host, port := listenEndpoint(listen)
	matched := false
	for address := range context.Addresses {
		ip, want := address[0], address[1]
		if port == want && (host == ip || (host == "" || host == "0.0.0.0" || host == "::") && context.Hosts[ip]) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	entries := map[string]bool{}
	for _, member := range servers {
		for _, entry := range member.entries {
			entries[entry] = true
		}
	}
	if len(entries) != 1 {
		return false
	}
	for _, member := range servers {
		if !strIn(member.plugin.Value("type"), "udp_server", "tcp_server") {
			return false
		}
	}
	entry := onlyElement(entries)
	if pluginAt(plugins, entry).Value("type") != "sequence" {
		return false
	}
	for tag := range reached {
		if tag != entry && strIn(pluginAt(plugins, tag).Value("type"), "sequence", "fallback") {
			return false
		}
	}
	names := map[string]bool{}
	for _, member := range servers {
		names[member.plugin.Value("tag").(string)] = true
	}
	list := asList(document.Value("plugins"))
	// owners: a reference outside plugins (or an unreadable path) counts as
	// foreign ("" here, Python's None).
	owners := func(tag string) map[string]bool {
		out := map[string]bool{}
		for _, edge := range referenceLocations(document, []string{tag}, false) {
			path := asList(item(edge, "path"))
			owner := "\x00None"
			if len(path) > 1 && path[0] == "plugins" {
				index, _ := path[1].(jsonx.Number).Int64()
				if text, ok := get(list[index], "tag", nil).(string); ok {
					owner = text
				}
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
		if pluginAt(plugins, tag).Value("type") != "forward" {
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

// InspectEntries is inspect_entries(document, plugins, config): check every
// DNS listener; return {side_entries, node_test_entries}.
func InspectEntries(document *jsonx.Object, config any) (report *jsonx.Object, err error) {
	defer catch(&err)
	return inspectEntries(document, routingPlugins(document), config), nil
}

func inspectEntries(document, plugins *jsonx.Object, config any) *jsonx.Object {
	var servers []*jsonx.Object
	for _, value := range plugins.Values() {
		plugin := value.(*jsonx.Object)
		if strings.HasSuffix(str(plugin.GetOr("type", "")), "_server") {
			servers = append(servers, plugin)
		}
	}
	if len(servers) == 0 {
		refuse("没有任何 DNS 监听入口（udp_server / tcp_server 等）")
	}
	type groupKey struct{ listen, entries string }
	var order []groupKey
	groups := map[groupKey][]serverMember{}
	groupEntries := map[groupKey][]string{}
	mainPorts := map[int]bool{53: true}
	hasMain := false
	for _, plugin := range servers {
		var listen any
		if args, ok := plugin.Value("args").(*jsonx.Object); ok && args != nil {
			listen = args.Value("listen")
		}
		text, ok := listen.(string)
		if !ok || strings.TrimSpace(text) == "" {
			text = "未填写监听地址"
		}
		name := "监听入口 " + str(plugin.Value("tag")) + "（" + text + "）"
		entries, ok := serverEntries(plugin)
		if !ok {
			refuse(name + " 的 entry 无法读取")
		}
		allMain := true
		for _, entry := range entries {
			if entry != "sequence_main" {
				allMain = false
			}
		}
		if allMain {
			hasMain = true
			if port, ok := listenPort(text); ok {
				mainPorts[port] = true
			}
			continue
		}
		key := groupKey{text, strings.Join(entries, "\x00")}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
			groupEntries[key] = entries
		}
		groups[key] = append(groups[key], serverMember{plugin, entries})
	}
	if !hasMain {
		refuse("没有任何监听入口进入主处理链 sequence_main")
	}
	var context EntryContext
	if len(order) > 0 {
		context = entryContext(config)
	}
	side, nodeTest := []any{}, []any{}
	for _, key := range order {
		members, entries := groups[key], groupEntries[key]
		listen := key.listen
		tags := []any{}
		names := []string{}
		for _, member := range members {
			tags = append(tags, member.plugin.Value("tag"))
			names = append(names, member.plugin.Value("tag").(string))
		}
		entryText := strings.Join(dedupe(entries), "、")
		entryItem := jsonx.Obj("tags", tags, "listen", listen, "entry", entryText)
		name := "监听入口 " + strings.Join(names, "、") + "（" + listen + "）"
		port, ok := listenPort(listen)
		if !ok || mainPorts[port] {
			portText := "无法确认监听端口"
			if ok {
				portText = "监听客户端 DNS 端口 " + itoa(port)
			}
			refuse(name+portText+"，但 entry 是 "+entryText+"，没有进入主处理链 sequence_main"+
				"（经这里查询的客户端不受设备策略控制）",
				"请在 OxiDNS 完整参数中把它的 entry 改为 sequence_main，或让它改用其他端口")
		}
		reached, problem := reach(plugins, entries)
		if problem != "" {
			refuse(name + " 的处理链" + problem + "，无法确认它是否经过主处理链")
		}
		if reached["sequence_main"] {
			refuse(name+" 的处理链 "+entryText+" 会间接调用主处理链 sequence_main，看不出设备策略是否生效",
				"请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链")
		}
		if legacyNodeTest(document, plugins, members, reached, listen, context) {
			legacy := entryItem.Clone()
			legacy.Set("kind", "legacy")
			nodeTest = append(nodeTest, legacy)
		} else {
			side = append(side, entryItem)
		}
	}
	return jsonx.Obj("side_entries", side, "node_test_entries", nodeTest)
}

// patchPolicies is _patch(document, previous, rows, config): the patched
// copy of document and the entry report.
func patchPolicies(document *jsonx.Object, previous, rows []any, config any) (*jsonx.Object, *jsonx.Object) {
	document = copyValue(document).(*jsonx.Object)
	plugins := routingPlugins(document)
	checkMain(document, plugins)
	report := inspectEntries(document, plugins, config)
	main := pluginAt(plugins, "sequence_main")
	args := main.Value("args").([]any)
	setArgs := func(items []any) {
		old := args
		main.Set("args", items)
		relinkList(document, old, items, map[unsafe.Pointer]bool{})
		args = items
	}

	old := policyPlugin(previous)
	owned := jsonx.NewObject()
	for _, tag := range []string{PolicyTag, DirectTag} {
		if plugins.Has(tag) {
			owned.Set(tag, plugins.Value(tag))
		}
	}
	var hooks []int
	for i, entry := range args {
		if jsonx.Equal(entry, policyHook()) {
			hooks = append(hooks, i)
		}
	}
	if old != nil {
		if !jsonx.Equal(owned, jsonx.Obj(PolicyTag, old, DirectTag, directPlugin())) || len(hooks) != 1 {
			fail("设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。")
		}
		index := hooks[0]
		setArgs(popCopy(args, index))
		if index != prefixEnd(args) {
			fail("设备策略的位置已被修改，请先恢复到分流规则之前。")
		}
	} else if owned.Len() > 0 || len(hooks) > 0 {
		fail("完整参数中存在同名设备策略插件，请先处理命名冲突。")
	}
	// Extra calls to owned plugins could turn a per-client rule into a global rule.
	remaining := []any{}
	for _, plugin := range asList(document.Value("plugins")) {
		if !strIn(get(plugin, "tag", nil), PolicyTag, DirectTag) {
			remaining = append(remaining, plugin)
		}
	}
	text, err := jsonx.Encode(remaining, jsonx.Options{EnsureASCII: true})
	if err != nil {
		raise(pyErr("TypeError", err.Error()))
	}
	if strings.Contains(string(text), PolicyTag) || strings.Contains(string(text), DirectTag) {
		fail("设备策略插件存在额外引用，请先在完整参数中处理。")
	}
	document.Set("plugins", remaining)
	if added := policyPlugin(rows); added != nil {
		setArgs(insertCopy(args, prefixEnd(args), policyHook()))
		document.Set("plugins", append(remaining, added, directPlugin()))
	}
	return document, report
}

// PatchDocument is patch_document(document, previous, rows, config): own
// exactly two plugins and one hook in sequence_main; refuse altered or
// ambiguous chains. Side listeners with their own chain are never
// modified. config nil is None.
func PatchDocument(document *jsonx.Object, previous, rows []any, config any) (patched *jsonx.Object, err error) {
	defer catch(&err)
	return patchDocument(document, previous, rows, config), nil
}

func patchDocument(document *jsonx.Object, previous, rows []any, config any) *jsonx.Object {
	patched, _ := patchPolicies(document, previous, rows, config)
	return patched
}

// ReadPolicies is read_policies(settings, config): {policies, editable,
// reason, side_entries}.
func ReadPolicies(settings, config any) (result *jsonx.Object, err error) {
	defer catch(&err)
	return readPolicies(settings, config), nil
}

func readPolicies(settings, config any) *jsonx.Object {
	names := storedNames(config)
	rows := []any{}
	for _, value := range normalizePolicies(get(settings, "device_policies", []any{})) {
		row := value.(*jsonx.Object).Clone()
		if name, ok := names.Get(row.GetString("ip")); ok {
			row.Set("name", name)
		}
		rows = append(rows, row)
	}
	var report *jsonx.Object
	caught := tryExcept(func() {
		current := prepareEditor(settings, config, "")
		_, report = patchPolicies(loadNativeYAML(item(current, "native_config"), DefaultNativeLabel), rows, rows, config)
	}, ClassSettings)
	if caught != nil {
		return jsonx.Obj("policies", rows, "editable", false, "reason", caught.Error(), "side_entries", []any{})
	}
	return jsonx.Obj("policies", rows, "editable", true, "reason", "", "side_entries", report.Value("side_entries"))
}

// ApplyPolicies is apply_policies(settings, config, rows, base_url): the
// prepared draft with the device policy patched in.
func ApplyPolicies(settings, config, rows any, baseURL string) (result *jsonx.Object, err error) {
	defer catch(&err)
	return applyPolicies(settings, config, rows, baseURL), nil
}

func applyPolicies(settings, config, value any, baseURL string) *jsonx.Object {
	rows := normalizePolicies(value)
	current := prepareEditor(settings, config, baseURL)
	previous := normalizePolicies(current.GetOr("device_policies", []any{}))
	document := patchDocument(loadNativeYAML(item(current, "native_config"), DefaultNativeLabel), previous, rows, config)
	current.Set("native_config", dump(document))
	current.Set("device_policies", rows)
	if template, ok := current.Value("template_settings").(*jsonx.Object); ok && template != nil {
		template.Set("device_policies", copyValue(rows))
	}
	return current
}
