package ros

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
)

const (
	Timeout            = 10 * time.Second
	rosPingTimeout     = 4 * time.Second
	PathRoutes         = "/ip/route"
	PathIPv6Routes     = "/ipv6/route"
	PathFilter         = "/ip/firewall/filter"
	PathNetwatch       = "/tool/netwatch"
	PathDHCPNetworks   = "/ip/dhcp-server/network"
	PathAddresses      = "/ip/firewall/address-list"
	PathDHCPServers    = "/ip/dhcp-server"
	PathDHCPLeases     = "/ip/dhcp-server/lease"
	PathDHCPOptions    = "/ip/dhcp-server/option"
	PathDHCPOptionSets = "/ip/dhcp-server/option/sets"
	PathDHCPMatchers   = "/ip/dhcp-server/matcher"
)

var (
	rosRoutePaths     = []string{PathRoutes, PathIPv6Routes}
	rosSingleRowPaths = []string{PathAddresses, PathIPv6AddressLists, PathRoutes, PathIPv6Routes}
	RouteFields       = map[string]bool{
		"dst-address": true, "gateway": true, "routing-table": true, "pref-src": true,
		"blackhole": true, "check-gateway": true, "distance": true, "scope": true,
		"target-scope": true, "vrf-interface": true, "suppress-hw-offload": true,
		"disabled": true, "comment": true,
	}
	ReadOnlyFields = map[string]bool{".id": true, "dynamic": true, "invalid": true, "bytes": true,
		"packets": true, "creation-time": true}
	RouteEvidence = map[string]bool{
		"dst-address": true, "gateway": true, "routing-table": true, "pref-src": true,
		"blackhole": true, "check-gateway": true, "distance": true, "scope": true,
		"target-scope": true, "vrf-interface": true, "suppress-hw-offload": true,
		"disabled": true, "comment": true, ".id": true, "immediate-gw": true,
		"active": true, "inactive": true, "connect": true, "ecmp": true, "invalid": true,
	}
	IDPattern = regexp.MustCompile(`^\*[0-9a-fA-F]+$`)
)

// Client is a Client 7 REST device client. One release or read uses a
// client from one goroutine at a time (the policy/DNS guard/IPv6 manifests
// are per-client state); only Ping may run concurrently.
type Client struct {
	endpoint string
	client   device.DeviceHTTP
	// password is only used to hide device echoes of it in manual route errors.
	password string

	// Batch sizes are per driver so independent routers can use their own limits.
	routeBatch int
	batchMin   int

	forwardFilterRows      []Row
	natListTouched         bool
	addressCache           map[string][]Row
	addressExpected        map[string][]Row
	addressReads           map[string]addressProof
	policyManifest         json.RawMessage
	policyManifestSet      bool
	dnsGuardManifest       json.RawMessage
	dnsGuardManifestSet    bool
	ipv6FakeIPManifest     json.RawMessage
	ipv6FakeIPManifestSet  bool
	ipv6RoutesManifest     json.RawMessage
	ipv6RoutesManifestSet  bool
	ipv6PDGuardManifest    json.RawMessage
	ipv6PDGuardManifestSet bool
}

// NewClient constructs a REST driver with requests-compatible device HTTP.
func NewClient(endpoint, username, password string, verifyTLS bool) *Client {
	driver := NewClientWithHTTP(endpoint, devhttp.New(devhttp.Config{
		VerifyTLS: verifyTLS, BasicAuth: true, Username: username, Password: password,
	}))
	driver.password = password
	return driver
}

// NewClientWithHTTP creates a driver with an injected device client.
func NewClientWithHTTP(endpoint string, client device.DeviceHTTP) *Client {
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), client: client,
		routeBatch: 100, batchMin: 4}
}

// Vendor returns the stored vendor name.
func (*Client) Vendor() string { return "routeros" }

// SupportsWrite reports that Client REST can apply release operations.
func (*Client) SupportsWrite() bool { return true }

// Close releases idle device connections.
func (d *Client) Close() {
	if d.client != nil {
		d.client.Close()
	}
}

// RenderScript is empty because Client supports direct writes.
func (*Client) RenderScript([]json.RawMessage) (string, error) { return "", nil }

// request sends one REST call; nil values means no body. The reply is
// valid JSON ("{}" for an empty success).
func (d *Client) Request(ctx context.Context, method, path string, values Row, timeout time.Duration) (json.RawMessage, error) {
	// A nil Row would be the body "null".
	if values == nil {
		return d.Send(ctx, method, path, nil, timeout)
	}
	return d.Send(ctx, method, path, values, timeout)
}

func (d *Client) Send(ctx context.Context, method, path string, body any, timeout time.Duration) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := d.client.Do(ctx, method, d.endpoint+"/rest"+path, body, timeout)
	if err != nil {
		kind := device.KindRouter
		if method != "GET" {
			kind = device.KindUncertainWrite
		}
		return nil, device.KindError(kind, fmt.Sprintf(messageROSConnectError, devhttp.ClassOf(err)))
	}
	if response.Status == 401 {
		return nil, device.Error(messageROSAuthentication)
	}
	if response.Status == 403 {
		return nil, device.Error(MessageROSForbidden + PermissionHelp)
	}
	if response.Status == 404 && method == "GET" {
		return nil, device.KindError(device.KindMissingResource, messageROSRESTUnavailable)
	}
	if response.Status == 404 && method == "DELETE" {
		return emptyReply, nil
	}
	if response.Status < 200 || response.Status >= 300 {
		body := AsRow(response.Body)
		detail := d.SafeDeviceText(strings.Join(device.NonEmpty([]string{body["message"], body["detail"]}), ": "))
		kind := device.KindRouter
		if method != "GET" && response.Status >= 500 {
			kind = device.KindUncertainWrite
		}
		message := fmt.Sprintf(messageROSHTTPStatus, method, path, response.Status)
		if detail != "" {
			message += "（" + detail + "）"
		}
		if permissionError.MatchString(detail) {
			message = fmt.Sprintf(messageROSPermissionFailure, PermissionHelp, message)
		}
		return nil, device.KindError(kind, message)
	}
	if response.Status == 204 {
		return emptyReply, nil
	}
	if !json.Valid(response.Body) {
		kind := device.KindRouter
		if method != "GET" {
			kind = device.KindUncertainWrite
		}
		return nil, device.KindError(kind, messageROSNotJSON)
	}
	return response.Body, nil
}

var emptyReply = json.RawMessage("{}")

// AsRow is an object reply as a row; nil when the reply is not an object.
func AsRow(raw json.RawMessage) Row {
	var row Row
	if ReplyKind(raw) != '{' || jsondecode.Decode(raw, &row) != nil {
		return nil
	}
	return row
}

// asRows is a list reply of objects; ok is false for any other shape.
func asRows(raw json.RawMessage) ([]Row, bool) {
	var rows []Row
	if ReplyKind(raw) != '[' || jsondecode.Decode(raw, &rows) != nil || !RowsOK(rows) {
		return nil, false
	}
	return rows, true
}

// nullMarker holds a JSON null in a field whose reader must refuse it (Row
// decoding otherwise treats null as absent). It is structured text, which
// no flag, list or name reader accepts, so the reader's own message stays.
const nullMarker = "{}"

// MarkNulls sets nullMarker for each of fields that is null in raw, the
// JSON object row was decoded from.
func MarkNulls(row Row, raw json.RawMessage, fields []string) {
	var members map[string]json.RawMessage
	if row == nil || jsondecode.Unmarshal(raw, &members) != nil {
		return
	}
	for _, field := range fields {
		if value, ok := members[field]; ok && string(bytes.TrimSpace(value)) == "null" {
			row[field] = nullMarker
		}
	}
}

// rosNullFields are the fields whose null every list read keeps as
// nullMarker: a reader of the table refused that null before rows were text,
// including the write paths (payload) that read rows through list.
var rosNullFields = map[string][]string{
	PathIPv6ND:        {"disabled", "dynamic", "invalid"},
	PathDHCPNetworks:  {"dns-none"},
	"/interface/list": {"include", "exclude"},
}

// strictList is list for a table whose readers refuse a null in fields.
func (d *Client) StrictList(ctx context.Context, path string, fields ...string) ([]Row, error) {
	return d.listMarked(ctx, path, fields)
}

// ReplyKind is the first byte of a JSON value.
func ReplyKind(raw json.RawMessage) byte {
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 {
		return 0
	}
	return raw[0]
}

var permissionError = regexp.MustCompile(`(?i)\bnot enough permissions\b`)

func (d *Client) Get(ctx context.Context, path string, timeout time.Duration) (json.RawMessage, error) {
	return d.Request(ctx, "GET", path, nil, timeout)
}

func (d *Client) List(ctx context.Context, path string) ([]Row, error) {
	return d.listMarked(ctx, path, nil)
}

// listMarked reads a table, keeping a null in fields (and in the table's
// rosNullFields) as nullMarker.
func (d *Client) listMarked(ctx context.Context, path string, fields []string) ([]Row, error) {
	value, err := d.Get(ctx, path, Timeout)
	if err != nil {
		return nil, err
	}
	rows, ok := asRows(value)
	if !ok {
		return nil, device.Error(fmt.Sprintf(device.MessageROSListFormat, path))
	}
	table, _, _ := strings.Cut(path, "?")
	if fields = append(slices.Clone(fields), rosNullFields[table]...); len(fields) > 0 {
		var items []json.RawMessage
		if jsondecode.Unmarshal(value, &items) != nil || len(items) != len(rows) {
			return nil, device.Error(fmt.Sprintf(device.MessageROSListFormat, path))
		}
		for i, item := range items {
			MarkNulls(rows[i], item, fields)
		}
	}
	if path == PathFilter {
		d.forwardFilterRows = rows
	}
	return rows, nil
}

// Ping reports whether one address answers two Client probes.
func (d *Client) Ping(ctx context.Context, address, iface string, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = rosPingTimeout
	}
	values := Row{"address": address, "count": "2", "interval": "300ms"}
	if iface != "" {
		values["arp-ping"], values["interface"] = "yes", iface
	}
	response, err := d.client.Do(ctx, "POST", d.endpoint+"/rest/ping", values, timeout)
	if err != nil {
		return false, device.Error(fmt.Sprintf(messageROSPingIncomplete, devhttp.ClassOf(err)))
	}
	if response.Status == 200 && ReplyKind(response.Body) == '[' {
		var items []json.RawMessage
		if jsondecode.Decode(response.Body, &items) == nil {
			for _, item := range items {
				if n, err := strconv.Atoi(AsRow(item)["received"]); err == nil && n > 0 {
					return true, nil
				}
			}
			return false, nil
		}
	}
	var detail string
	if row := AsRow(response.Body); row != nil {
		detail = device.FirstNonEmpty(row["detail"], row["message"])
	}
	if permissionError.MatchString(detail) {
		return false, device.KindError(device.KindPingNotPermitted, fmt.Sprintf(messageROSPingPermission, detail))
	}
	message := fmt.Sprintf(messageROSPingHTTPStatus, response.Status)
	if detail != "" {
		message += "（" + detail + "）"
	}
	return false, device.Error(message)
}

// Probe checks Client version and which of its read-only surfaces are exposed.
func (d *Client) Probe(ctx context.Context) (*device.RouterInfo, error) {
	value, err := d.Get(ctx, "/system/resource", Timeout)
	if err != nil {
		return nil, err
	}
	resource := AsRow(value)
	if resource == nil {
		return nil, device.Error(messageROSSystemInfoFormat)
	}
	version := resource["version"]
	match := versionPattern.FindStringSubmatch(version)
	if len(match) != 3 {
		return nil, device.Error(fmt.Sprintf(messageROSVersionRequired, device.FirstNonEmpty(version, device.MessageUnknown)))
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 7 || major == 7 && minor < 1 {
		return nil, device.Error(fmt.Sprintf(messageROSVersionRequired, device.FirstNonEmpty(version, device.MessageUnknown)))
	}
	capabilities := []string{}
	for _, item := range []struct{ path, capability string }{
		{PathRoutes, "routes"}, {PathNAT, "nat_bypass"}, {PathAddresses + "?list=cf-probe", "address_sets"},
	} {
		if _, err := d.List(ctx, item.path); err == nil {
			capabilities = append(capabilities, item.capability)
		} else if !device.IsRouterError(err) {
			return nil, err
		}
	}
	return &device.RouterInfo{Vendor: "routeros", Version: version, Model: resource["board-name"],
		Capabilities: capabilities}, nil
}

var versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)`)

// ReadDHCPDNS reads DHCP networks and every Client option layer that can
// replace network option 6. Dynamic leases without custom options are ignored.
func (d *Client) ReadDHCPDNS(ctx context.Context) (*device.DhcpDnsState, error) {
	paths := []string{PathDHCPNetworks, PathDHCPServers, PathDHCPLeases, PathDHCPOptions, PathDHCPOptionSets, PathDHCPMatchers}
	read := make([][]Row, len(paths))
	for i, path := range paths {
		// Python refused a null option or DNS list.
		rows, err := d.StrictList(ctx, path, "dhcp-option", "dhcp-option-set", "options", "dns-server", "dns-none")
		if err != nil {
			return nil, err
		}
		read[i] = rows
	}
	networks, servers, leases := read[0], read[1], read[2]
	options, optionSets, matchers := read[3], read[4], read[5]
	activeServers := []Row{}
	globals := []string{}
	for _, server := range servers {
		if !Flag(server["disabled"]) {
			activeServers = append(activeServers, server)
		}
	}
	if len(activeServers) == 0 {
		globals = append(globals, messageROSDHCPNoActiveServer)
	}
	for _, server := range activeServers {
		label := fmt.Sprintf(messageROSDHCPServiceLabel, device.FirstNonEmpty(device.FirstNonEmpty(server["name"], server[".id"]), device.MessageUnknown))
		if !server.Set("name") || Flag(server["invalid"]) {
			globals = append(globals, fmt.Sprintf(messageROSDHCPIncomplete, label))
		}
		radius := strings.ToLower(server.TextOr("use-radius", "no"))
		if radius != "no" && radius != "false" && radius != "accounting" {
			globals = append(globals, fmt.Sprintf(messageROSDHCPRadiusServer, label))
		}
		blockers, err := dhcpOverrides(server, label, options, optionSets)
		if err != nil {
			return nil, err
		}
		globals = append(globals, blockers...)
	}
	disabledServers := map[string]bool{}
	for _, server := range servers {
		if Flag(server["disabled"]) {
			disabledServers[server.Text("name")] = true
		}
	}
	for _, matcher := range matchers {
		if Flag(matcher["disabled"]) || disabledServers[matcher.Text("server")] {
			continue
		}
		label := fmt.Sprintf(messageROSDHCPMatcherLabel, device.FirstNonEmpty(device.FirstNonEmpty(matcher["name"], matcher[".id"]), device.MessageUnknown))
		matcherOptions := Row{"dhcp-option-set": ""}
		if value, ok := matcher["option-set"]; ok {
			matcherOptions["dhcp-option-set"] = value
		}
		blockers, err := dhcpOverrides(matcherOptions, label, options, optionSets)
		if err != nil {
			return nil, err
		}
		globals = append(globals, blockers...)
	}
	overrideLeases := []Row{}
	for _, lease := range leases {
		if Flag(lease["disabled"]) {
			continue
		}
		optionNames, err := dhcpNamesField(lease, "dhcp-option")
		if err != nil {
			return nil, err
		}
		setNames, err := dhcpNamesField(lease, "dhcp-option-set")
		if err != nil {
			return nil, err
		}
		if len(optionNames) > 0 || len(setNames) > 0 || Flag(lease["radius"]) {
			overrideLeases = append(overrideLeases, lease)
		}
	}

	entries := make([]*device.DhcpDnsEntry, 0, len(networks))
	seenIDs := map[string]bool{}
	for _, row := range networks {
		id := row.ID()
		if id == "" || seenIDs[id] {
			return nil, device.Error(messageROSDHCPInvalidNetwork)
		}
		seenIDs[id] = true
		network, ok := netipx.ParseNetwork4(row.Text("address"), false)
		if !ok {
			return nil, device.Error(messageROSDHCPNetworkAddress)
		}
		attributes, err := dhcpNetworkAttributes(row)
		if err != nil {
			return nil, err
		}
		blockers := slices.Clone(globals)
		if Flag(row["dynamic"]) || Flag(row["invalid"]) {
			blockers = append(blockers, messageROSDHCPNetworkBlocked)
		}
		networkLabel := fmt.Sprintf(messageROSDHCPNetworkLabel, network.String())
		networkBlockers, err := dhcpOverrides(row, networkLabel, options, optionSets)
		if err != nil {
			return nil, err
		}
		blockers = append(blockers, networkBlockers...)
		for _, lease := range overrideLeases {
			if disabledServers[lease.Text("server")] {
				continue
			}
			address, valid := netipx.ParseAddr4(lease.Text("address"))
			if valid && !address.IsUnspecified() && !network.Contains(address) {
				continue
			}
			label := fmt.Sprintf(messageROSDHCPLeaseLabel, device.FirstNonEmpty(device.FirstNonEmpty(lease["address"], lease[".id"]), device.MessageUnknown))
			if Flag(lease["radius"]) {
				blockers = append(blockers, fmt.Sprintf(messageROSDHCPRadiusLease, label))
			}
			leaseBlockers, err := dhcpOverrides(lease, label, options, optionSets)
			if err != nil {
				return nil, err
			}
			blockers = append(blockers, leaseBlockers...)
		}
		dnsNames, err := dhcpNames(attributes["dns-server"])
		if err != nil {
			return nil, err
		}
		dns := make([]string, 0, len(dnsNames))
		for _, name := range dnsNames {
			address, valid := netipx.ParseAddr4(name)
			if !valid {
				return nil, device.Error(fmt.Sprintf(messageROSDHCPDNSAddress, network.String()))
			}
			dns = append(dns, address.String())
		}
		if Flag(attributes["dns-none"]) {
			dns = []string{}
		}
		attrs, err := json.Marshal(attributes)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &device.DhcpDnsEntry{ID: id, Network: network.String(), DNS: dns,
			Enabled: !Flag(row["disabled"]), Attributes: attrs, Blockers: device.Unique(blockers), DNSExplicit: true})
	}
	evidence := map[string][]Row{
		"networks": dhcpEvidenceNetworks(networks),
		"servers": dhcpEvidence(servers, []string{".id", "name", "disabled", "invalid", "dynamic", "interface",
			"address-pool", "relay", "use-radius", "dhcp-option", "dhcp-option-set"}),
		"leases":      dhcpEvidence(overrideLeases, []string{".id", "address", "server", "dynamic", "radius", "dhcp-option", "dhcp-option-set"}),
		"options":     dhcpEvidence(options, []string{".id", "name", "code", "value", "force"}),
		"option_sets": dhcpEvidence(optionSets, []string{".id", "name", "options"}),
		"matchers":    dhcpEvidence(matchers, []string{".id", "name", "disabled", "server", "code", "matching-type", "value", "option-set", "address-pool"}),
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	return &device.DhcpDnsState{Entries: entries, Evidence: evidenceJSON}, nil
}

func dhcpNetworkAttributes(row Row) (Row, error) {
	attrs := make(Row, len(row)+2)
	for key, value := range row {
		if !strings.HasPrefix(key, ".") && key != "dynamic" && key != "invalid" {
			attrs[key] = value
		}
	}
	if _, ok := attrs["dns-server"]; !ok {
		attrs["dns-server"] = ""
	}
	if _, ok := attrs["dns-none"]; !ok {
		attrs["dns-none"] = "false"
	}
	if !slices.Contains([]string{"true", "false", "yes", "no"}, attrs["dns-none"]) {
		return nil, device.Error(messageROSDHCPDNSNone)
	}
	return attrs, nil
}

func dhcpNames(value string) ([]string, error) {
	items, ok := TextList(value)
	if !ok {
		return nil, device.Error(messageROSDHCPNameList)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && item != "none" {
			out = append(out, item)
		}
	}
	return out, nil
}

func dhcpNamesField(row Row, key string) ([]string, error) {
	value, ok := row[key]
	if !ok {
		return []string{}, nil
	}
	return dhcpNames(value)
}

func dhcpOverrides(row Row, label string, options, optionSets []Row) ([]string, error) {
	blockers := []string{}
	references, err := dhcpNamesField(row, "dhcp-option")
	if err != nil {
		return nil, err
	}
	setNames, err := dhcpNamesField(row, "dhcp-option-set")
	if err != nil {
		return nil, err
	}
	for _, name := range setNames {
		matches := matchingRows(optionSets, name)
		if len(matches) != 1 || !matches[0].Has("options") {
			blockers = append(blockers, fmt.Sprintf(messageROSDHCPOptionSet, label, name))
			continue
		}
		setOptions, err := dhcpNamesField(matches[0], "options")
		if err != nil {
			return nil, err
		}
		references = append(references, setOptions...)
	}
	seen := map[string]bool{}
	for _, name := range references {
		if seen[name] {
			continue
		}
		seen[name] = true
		matches := matchingRows(options, name)
		if len(matches) != 1 {
			blockers = append(blockers, fmt.Sprintf(messageROSDHCPOption, label, name))
			continue
		}
		code, known := optionCode(matches[0]["code"])
		if known && code == 6 {
			blockers = append(blockers, fmt.Sprintf(messageROSDHCPOption6, label, name))
		} else if !known || code < 1 || code > 254 {
			blockers = append(blockers, fmt.Sprintf(messageROSDHCPOptionUnknown, label, name))
		}
	}
	return blockers, nil
}

func matchingRows(rows []Row, name string) []Row {
	matches := []Row{}
	for _, row := range rows {
		if row.Text("name") == name || row.Text(".id") == name {
			matches = append(matches, row)
		}
	}
	return matches
}

func optionCode(value string) (int64, bool) {
	if value == "vendor-specific" {
		return 43, true
	}
	if value == "" || strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil
}

func dhcpEvidenceNetworks(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		attrs, _ := dhcpNetworkAttributes(row)
		for _, key := range []string{".id", "dynamic", "invalid"} {
			if value, ok := row[key]; ok {
				attrs[key] = value
			}
		}
		out = append(out, attrs)
	}
	return out
}

func dhcpEvidence(rows []Row, fields []string) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		item := make(Row)
		for _, key := range fields {
			if value, ok := row[key]; ok {
				item[key] = value
			}
		}
		out = append(out, item)
	}
	return out
}

// comparable is the normalized form of one field value: flags as
// "true"/"false", networks canonical. Row decoding already turned non-text
// JSON into its compact form, so such values still compare by value.
func Comparable(key, text string) string {
	if key == "dns-none" && text == "" {
		return "false"
	}
	if slices.Contains([]string{"disabled", "blackhole", "log", "suppress-hw-offload", "dns-none"}, key) {
		switch strings.ToLower(text) {
		case "true", "yes":
			return "true"
		case "false", "no":
			return "false"
		}
	}
	if slices.Contains([]string{"src-address", "dst-address", "address"}, key) {
		if network, ok := netipx.ParseNetwork(text, false); ok {
			return network.String()
		}
	}
	return text
}

func matches(row, values Row) bool {
	for key, value := range values {
		if key != "place-before" && Comparable(key, row[key]) != Comparable(key, value) {
			return false
		}
	}
	return true
}

func ReferencedAddressLists(rows []Row) []string {
	names := map[string]bool{}
	for _, row := range rows {
		for _, key := range []string{"src-address-list", "dst-address-list"} {
			name := strings.TrimLeft(row[key], "!")
			if name != "" {
				names[name] = true
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func SelectFields(rows []Row, fields map[string]bool) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		selected := make(Row)
		for key, value := range row {
			if fields[key] {
				selected[key] = value
			}
		}
		out = append(out, selected)
	}
	return out
}

func WithoutCounters(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		selected := make(Row, len(row))
		for key, value := range row {
			if key != "bytes" && key != "packets" {
				selected[key] = value
			}
		}
		out = append(out, selected)
	}
	return out
}

// SetPolicyManifest installs the last Client policy manifest for readback
// filtering. The policy-routing module owns the contents of this value.
func (d *Client) SetPolicyManifest(raw json.RawMessage) {
	d.policyManifest = slices.Clone(raw)
	d.policyManifestSet = true
}

// SetDNSGuardManifest installs the DNS guard ownership snapshot for NAT
// assessment. The DNS guard module owns the contents of this value.
func (d *Client) SetDNSGuardManifest(raw json.RawMessage) {
	d.dnsGuardManifest = slices.Clone(raw)
	d.dnsGuardManifestSet = true
}

type ResourceManifest struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Role   string `json:"role"`
	Config Row    `json:"config"`
}

type rosManifest struct {
	Resources []ResourceManifest `json:"resources"`
}

func OwnedHairpinIDs(raw json.RawMessage, rules []Row) map[string]bool {
	var manifest rosManifest
	if jsondecode.Unmarshal(raw, &manifest) != nil {
		return nil
	}
	ids := map[string]bool{}
	for _, entry := range manifest.Resources {
		if !strings.HasPrefix(entry.Role, "hairpin") {
			continue
		}
		for _, row := range rules {
			if row.ID() == entry.ID && maps.Equal(StableRow(PathNAT, row), entry.Config) {
				ids[entry.ID] = true
				break
			}
		}
	}
	return ids
}

var rosPolicyRuntime = map[string]bool{
	".id": true, "bytes": true, "packets": true, "active": true, "inactive": true,
	"immediate-gw": true, "used": true, "creation-time": true, "belongs-to": true,
	"hw-offloaded": true, "debug.fwp-ptr": true, "actual-interface": true,
	"uptime": true, "last-updated": true, "invalid": true, "timeout": true,
}

func ConfigROSRow(row Row) Row {
	config := make(Row)
	for key, value := range CloneRow(row) {
		if !rosPolicyRuntime[key] {
			config[key] = value
		}
	}
	return config
}

func StableRow(path string, row Row) Row {
	fields := map[string]bool{}
	if path == PathNetwatch {
		for _, key := range []string{"name", "host", "port", "type", "interval", "timeout", "startup-delay",
			"up-script", "down-script", "comment", "disabled"} {
			fields[key] = true
		}
	}
	stable := make(Row)
	for key, value := range row {
		if path == PathNetwatch {
			if !fields[key] {
				continue
			}
		} else if strings.HasPrefix(key, ".") || slices.Contains([]string{"dynamic", "invalid", "bytes", "packets", "creation-time"}, key) {
			continue
		}
		stable[key] = Comparable(key, value)
	}
	return stable
}

func (d *Client) FilterPolicyMangle(state *NatContext) error {
	var manifest rosManifest
	if err := devhttp.DecodeJSON(d.policyManifest, &manifest); err != nil {
		return device.ErrNotPorted
	}
	proven := map[string]bool{}
	for _, entry := range manifest.Resources {
		if entry.Path != PathMangle {
			continue
		}
		for _, row := range state.Mangle {
			if row.ID() == entry.ID && SameRow(ConfigROSRow(row), ConfigROSRow(entry.Config)) {
				proven[entry.ID] = true
				break
			}
		}
	}
	state.Mangle = slices.DeleteFunc(state.Mangle, func(row Row) bool { return proven[row.ID()] })
	return nil
}

// ROSChange is one REST write or compensation record.
type ROSChange struct {
	Method    string          `json:"method"`
	Path      string          `json:"path"`
	ID        string          `json:"id,omitempty"`
	Values    Row             `json:"values,omitempty"`
	Expected  json.RawMessage `json:"expected,omitempty"`
	Unchanged json.RawMessage `json:"unchanged,omitempty"`
	KnownIDs  []string        `json:"known_ids,omitempty"`
}

// ROSUndo is the undo value returned from an applied Client operation.
type ROSUndo struct {
	Kind    string      `json:"kind"`
	Changes []ROSChange `json:"changes"`
}

// Check describes a post-write readback verification against RouterOS.
type Check struct {
	Created Row    // response-proven configuration; private, never part of an undo wire record
	Path    string `json:"path"`
	ID      string `json:"id"`
	Values  Row    `json:"values,omitempty"`
	Absent  bool   `json:"absent,omitempty"`
}

type rosScope struct {
	ID      string `json:"id"`
	Network string `json:"network"`
	Gateway string `json:"gateway"`
}

type Operation struct {
	Kind           string               `json:"kind"`
	ID             string               `json:"id"`
	CIDR           string               `json:"cidr"`
	Gateway        string               `json:"gateway"`
	Comment        string               `json:"comment"`
	Src            []string             `json:"src"`
	Dst            []string             `json:"dst"`
	AddDst         []string             `json:"add_dst"`
	OutInterface   string               `json:"out_interface"`
	DNS            []string             `json:"dns"`
	Scope          *rosScope            `json:"scope"`
	Expected       json.RawMessage      `json:"expected"`
	CheckedDHCPDNS *device.DhcpDnsState `json:"checked_dhcp_dns"`
	Changes        []ROSChange          `json:"changes"`
}

// applyBase applies one base Client release operation (routes, NAT, DHCP,
// ros_restore) and returns its compensation.
func (d *Client) ApplyBase(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var op Operation
	if err := jsondecode.Unmarshal(raw, &op); err != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	var result any
	switch op.Kind {
	case "ros_restore":
		undo, err := d.Restore(ctx, op.Changes)
		if err != nil {
			return nil, err
		}
		result = undo
	case "dhcp_dns_update":
		undo, err := d.applyDHCPDNSUpdate(ctx, op)
		if err != nil {
			return nil, err
		}
		result = undo
	case "route_add":
		var err error
		result, err = d.Transaction(ctx, []ROSChange{{Method: "PUT", Path: PathRoutes, Values: Row{
			"dst-address": op.CIDR, "gateway": op.Gateway, "comment": op.Comment, "disabled": "false",
		}}})
		if err != nil {
			return nil, err
		}
	case "route_delete":
		var err error
		result, err = d.Transaction(ctx, []ROSChange{{Method: "DELETE", Path: PathRoutes, ID: op.ID}})
		if err != nil {
			return nil, err
		}
	case "route_comment":
		var err error
		result, err = d.Transaction(ctx, []ROSChange{{Method: "PATCH", Path: PathRoutes, ID: op.ID,
			Values: Row{"comment": op.Comment}}})
		if err != nil {
			return nil, err
		}
	case "nat_update":
		var err error
		result, err = d.applyNATUpdate(ctx, op)
		if err != nil {
			return nil, err
		}
	case "nat_add":
		var err error
		result, err = d.applyNATAdd(ctx, op)
		if err != nil {
			return nil, err
		}
	default:
		return nil, device.Error(fmt.Sprintf(device.MessageUnsupportedOperation, op.Kind))
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (d *Client) applyDHCPDNSUpdate(ctx context.Context, op Operation) (ROSUndo, error) {
	if len(op.DNS) < 1 || len(op.DNS) > 2 {
		return ROSUndo{}, device.Error(messageDHCPDNSCount)
	}
	dns := make([]string, 0, len(op.DNS))
	for _, value := range op.DNS {
		address, ok := netipx.ParseAddr4(value)
		if !ok {
			return ROSUndo{}, device.Error(messageDHCPDNSAddressInvalid)
		}
		dns = append(dns, address.String())
	}
	if op.Scope == nil || op.Scope.ID == "" || op.Scope.Network == "" || op.Scope.Gateway == "" || op.Scope.ID != op.ID {
		return ROSUndo{}, device.Error(messageDHCPDNSScope)
	}
	checked := op.CheckedDHCPDNS
	if checked == nil {
		var err error
		checked, err = d.ReadDHCPDNS(ctx)
		if err != nil {
			return ROSUndo{}, err
		}
	}
	entry, err := device.CheckedDNSTarget(device.DNSCheck{ID: op.Scope.ID, Network: op.Scope.Network, Gateway: op.Scope.Gateway}, checked)
	if err != nil {
		return ROSUndo{}, err
	}
	// The plan always records the previewed attributes; without them there is
	// nothing to prove the network unchanged, so the write is refused.
	expected, present := decodeRow(op.Expected)
	if !present || !SameRow(dhcpAttributesRow(entry), expected) {
		return ROSUndo{}, device.Error(messageDHCPDNSDrift)
	}
	return d.Transaction(ctx, []ROSChange{{Method: "PATCH", Path: PathDHCPNetworks, ID: entry.ID,
		Values: Row{"dns-server": strings.Join(dns, ","), "dns-none": "false"}}})
}

func (d *Client) applyNATUpdate(ctx context.Context, op Operation) (ROSUndo, error) {
	row, err := d.Row(ctx, PathNAT, op.ID)
	if err != nil {
		return ROSUndo{}, err
	}
	original := Payload(PathNAT, row)
	if original == nil {
		return ROSUndo{}, PayloadError(PathNAT, row)
	}
	expected, present := decodeRow(op.Expected)
	if present && !SameRow(original, expected) {
		return ROSUndo{}, device.Error(messageNATRuleDrift)
	}
	if !row.Is("chain", "srcnat") || !row.Is("action", "accept") {
		return ROSUndo{}, device.Error(messageNATTargetInvalid)
	}
	changes := []ROSChange{{Method: "PATCH", Path: PathNAT, ID: op.ID, Values: Row{"comment": op.Comment}}}
	seen := map[string]bool{}
	for _, cidr := range op.AddDst {
		if seen[cidr] {
			continue
		}
		seen[cidr] = true
		values := CloneRow(original)
		values["dst-address"], values["comment"], values["place-before"] = cidr, op.Comment, row.ID()
		if op.OutInterface != "" {
			values["out-interface"] = op.OutInterface
		}
		changes = append(changes, ROSChange{Method: "PUT", Path: PathNAT, Values: values})
	}
	return d.Transaction(ctx, changes)
}

func (d *Client) applyNATAdd(ctx context.Context, op Operation) (ROSUndo, error) {
	rows, err := d.List(ctx, PathNAT)
	if err != nil {
		return ROSUndo{}, err
	}
	position := Row{}
	if len(rows) > 0 {
		position["place-before"] = rows[0].ID()
	}
	sources, err := CollapseRouterNetworks(op.Src)
	if err != nil {
		return ROSUndo{}, err
	}
	if len(op.Src) > 0 && len(sources) == 0 {
		return ROSUndo{}, device.Error(messageNATSourceInvalid)
	}
	if len(sources) == 0 {
		sources = []string{""}
	}
	changes := []ROSChange{}
	uniqueDst := device.Unique(op.Dst)
	for _, source := range sources {
		for _, cidr := range uniqueDst {
			values := Row{"chain": "srcnat", "action": "accept", "disabled": "false",
				"dst-address": cidr, "comment": op.Comment}
			for key, value := range position {
				values[key] = value
			}
			if source != "" {
				values["src-address"] = source
			}
			if op.OutInterface != "" {
				values["out-interface"] = op.OutInterface
			}
			changes = append(changes, ROSChange{Method: "PUT", Path: PathNAT, Values: values})
		}
	}
	return d.Transaction(ctx, changes)
}

func CollapseRouterNetworks(values []string) ([]string, error) {
	collapsed, err := netipx.Collapse(device.Networks(values))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(collapsed))
	for i, network := range collapsed {
		out[i] = network.String()
	}
	return out, nil
}

func dhcpAttributesRow(entry *device.DhcpDnsEntry) Row {
	if entry == nil || len(entry.Attributes) == 0 {
		return nil
	}
	var row Row
	_ = json.Unmarshal(entry.Attributes, &row)
	return row
}

func decodeRow(raw json.RawMessage) (Row, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	if string(raw) == "null" {
		return nil, true
	}
	var row Row
	if json.Unmarshal(raw, &row) != nil {
		return nil, true
	}
	return row, true
}

// CloneRow copies a row; the copy is never nil.
func CloneRow(row Row) Row {
	if row == nil {
		return Row{}
	}
	return maps.Clone(row)
}

func isSingleRowPath(path string) bool { return slices.Contains(rosSingleRowPaths, path) }
func isRoutePath(path string) bool     { return slices.Contains(rosRoutePaths, path) }

func (d *Client) Find(ctx context.Context, path, id string) (Row, error) {
	if !isSingleRowPath(path) {
		rows, err := d.List(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.ID() == id {
				return row, nil
			}
		}
		return nil, nil
	}
	value, err := d.Get(ctx, path+"/"+EscapePathID(id), Timeout)
	if err != nil {
		var rosErr *device.RouterError
		if errors.As(err, &rosErr) && rosErr.Kind == device.KindMissingResource {
			return nil, nil
		}
		return nil, err
	}
	row := AsRow(value)
	if !row.Is(".id", id) {
		return nil, device.Error(fmt.Sprintf(messageROSObjectFormat, path))
	}
	return row, nil
}

func EscapePathID(id string) string {
	return strings.ReplaceAll(url.PathEscape(id), "%2A", "*")
}

func (d *Client) Row(ctx context.Context, path, id string) (Row, error) {
	row, err := d.Find(ctx, path, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, device.Error(fmt.Sprintf(messageROSNotFoundAfterManualDelete, path, id))
	}
	return row, nil
}

func Payload(path string, row Row) Row {
	if Flag(row["dynamic"]) {
		return nil
	}
	if path == "/ipv6/nd" {
		for _, key := range []string{"disabled", "dynamic", "invalid"} {
			if flag, err := IPv6DNSFlag(row, key); err != nil || flag && key != "disabled" {
				return nil
			}
		}
		if Flag(row["invalid"]) {
			return nil
		}
		_ = row["disabled"] // retained in the returned payload below
	}
	if isRoutePath(path) {
		return SelectFields([]Row{row}, RouteFields)[0]
	}
	if path == "/system/script" {
		return SelectFields([]Row{row}, device.StringSet("name", "source", "policy", "comment", "dont-require-permissions"))[0]
	}
	if path == "/system/scheduler" {
		return SelectFields([]Row{row}, device.StringSet("name", "on-event", "interval", "start-time", "start-date", "policy", "comment", "disabled"))[0]
	}
	if path == "/ipv6/dhcp-client" {
		out := make(Row)
		for key, value := range row {
			if !strings.HasPrefix(key, ".") && !slices.Contains([]string{"status", "prefix", "dhcp-server-v6", "duid", "invalid", "dynamic", "expires-after", "address"}, key) {
				out[key] = value
			}
		}
		if _, ok := out["script"]; !ok {
			out["script"] = ""
		}
		return out
	}
	if path == PathDHCPNetworks {
		attrs, _ := dhcpNetworkAttributes(row)
		return attrs
	}
	if path == PathNetwatch {
		return SelectFields([]Row{row}, device.StringSet("name", "host", "port", "type", "interval", "timeout", "startup-delay", "up-script", "down-script", "comment", "disabled"))[0]
	}
	out := make(Row)
	for key, value := range row {
		if !ReadOnlyFields[key] && !strings.HasPrefix(key, ".") {
			out[key] = value
		}
	}
	return out
}

// PayloadError is why payload refused row: Client-generated rows (and
// invalid IPv6 ND entries) are never taken over, changed or removed.
func PayloadError(path string, row Row) error {
	if path == PathIPv6ND {
		for _, key := range []string{"disabled", "dynamic", "invalid"} {
			if _, err := IPv6DNSFlag(row, key); err != nil {
				return err
			}
		}
	}
	if path == PathIPv6ND && !Flag(row["dynamic"]) {
		dynamic, _ := IPv6DNSFlag(row, "dynamic")
		invalid, _ := IPv6DNSFlag(row, "invalid")
		if dynamic || invalid {
			return device.Error(messageInvalidIPv6ND)
		}
	}
	return device.Error(messageROSDynamicObject)
}

func (d *Client) OwnershipSnapshot(path string, row Row) Row {
	values := Payload(path, row)
	if values == nil {
		return nil
	}
	defaults := Row{"disabled": "false", "comment": ""}
	if isRoutePath(path) {
		for key, value := range (Row{"routing-table": "main", "distance": "1", "scope": "30", "target-scope": "10",
			"check-gateway": "none", "pref-src": "", "suppress-hw-offload": "false", "blackhole": "false", "vrf-interface": ""}) {
			defaults[key] = value
		}
	}
	for key, value := range values {
		defaults[key] = value
	}
	out := make(Row, len(defaults))
	for key, value := range defaults {
		if key != "place-before" {
			out[key] = Comparable(key, value)
		}
	}
	return out
}

func (d *Client) Perform(ctx context.Context, change ROSChange, undo *[]ROSChange, restoring bool, beforeCreate ...func([]Row) error) (Check, error) {
	method, path := change.Method, change.Path
	values := CloneRow(change.Values)
	if values == nil {
		values = Row{}
	}
	if method == "RESTORE" {
		var current Row
		var rows []Row
		if isSingleRowPath(path) {
			var err error
			current, err = d.Find(ctx, path, change.ID)
			if err != nil {
				return Check{}, err
			}
			if isRoutePath(path) && current == nil {
				rows, err = d.Scoped(ctx, path, values)
				if err != nil {
					return Check{}, err
				}
			}
		} else {
			var err error
			rows, err = d.List(ctx, path)
			if err != nil {
				return Check{}, err
			}
			current = FindRow(rows, change.ID)
		}
		if current != nil {
			if Payload(path, current) == nil {
				return Check{}, PayloadError(path, current)
			}
			if !RowsEqual(d.OwnershipSnapshot(path, current), change.Expected) {
				return Check{}, device.Error(messageRollbackChanged)
			}
			delete(values, "place-before")
			return Check{Path: path, ID: change.ID, Values: values}, nil
		}
		if isRoutePath(path) && hasUnexpectedRoute(rows, values, change.KnownIDs) {
			return Check{}, device.Error(messageRollbackRouteConflict)
		}
		if values.Has("place-before") && FindRow(rows, values["place-before"]) == nil {
			return Check{}, device.Error(messageRollbackNATOrder)
		}
		return d.Perform(ctx, ROSChange{Method: "PUT", Path: path, Values: values}, undo, restoring, beforeCreate...)
	}

	if method == "PUT" {
		var beforeRows []Row
		var err error
		if isSingleRowPath(path) {
			beforeRows, err = d.Scoped(ctx, path, values)
		} else {
			beforeRows, err = d.List(ctx, path)
		}
		if err != nil {
			return Check{}, err
		}
		for _, guard := range beforeCreate {
			if err := guard(beforeRows); err != nil {
				return Check{}, err
			}
		}
		before := RowIDs(beforeRows)
		result, err := d.Request(ctx, method, path, values, Timeout)
		if err != nil {
			if device.RouterErrorKind(err) == device.KindUncertainWrite {
				return Check{}, device.RollbackError(fmt.Sprintf(messageUncertainWrite, err))
			}
			return Check{}, err
		}
		newRow := AsRow(result)
		rowID, idOK := newRow.Str(".id")
		if !idOK || !IDPattern.MatchString(rowID) || before[rowID] || isSingleRowPath(path) && !inScope(path, newRow, values) {
			return Check{}, device.RollbackError(fmt.Sprintf(device.MessageUncertainRouteWrite, path))
		}
		expected := d.OwnershipSnapshot(path, MergeRows(values, newRow))
		*undo = append(*undo, ROSChange{Method: "DELETE", Path: path, ID: rowID, Expected: MarshalRow(expected)})
		return Check{Path: path, ID: rowID, Values: values, Created: CloneRow(newRow)}, nil
	}

	id := change.ID
	resource := path + "/" + EscapePathID(id)
	var row Row
	var rows []Row
	if isSingleRowPath(path) {
		var err error
		row, err = d.Find(ctx, path, id)
		if err != nil {
			return Check{}, err
		}
		if row != nil {
			rows = []Row{row}
		}
		if isRoutePath(path) && row != nil && method == "DELETE" {
			neighbors, err := d.Scoped(ctx, path, row)
			if err != nil {
				return Check{}, err
			}
			for _, neighbor := range neighbors {
				if neighbor.ID() != id {
					rows = append(rows, neighbor)
				}
			}
		}
	} else {
		var err error
		rows, err = d.List(ctx, path)
		if err != nil {
			return Check{}, err
		}
		row = FindRow(rows, id)
	}
	if row == nil && method == "DELETE" {
		if restoring {
			current, err := d.Get(ctx, resource, Timeout)
			if err != nil {
				if device.RouterErrorKind(err) == device.KindMissingResource {
					return Check{Path: path, ID: id, Absent: true}, nil
				}
				return Check{}, err
			}
			row = AsRow(current)
			if !row.Is(".id", id) {
				return Check{}, device.Error(messageRollbackUnreadable)
			}
		} else {
			return Check{}, device.Error(fmt.Sprintf(messageROSNotFound, path, id))
		}
	}
	if row == nil {
		return Check{}, device.Error(fmt.Sprintf(messageROSNotFound, path, id))
	}
	original := Payload(path, row)
	if original == nil {
		return Check{}, PayloadError(path, row)
	}
	snapshot := d.OwnershipSnapshot(path, row)
	unchanged, unchangedSet := decodeRow(change.Unchanged)
	if restoring && unchangedSet && maps.Equal(snapshot, unchanged) {
		return Check{Path: path, ID: id, Values: values}, nil
	}
	expected, expectedSet := decodeRow(change.Expected)
	if expectedSet && !maps.Equal(snapshot, expected) {
		return Check{}, device.Error(messageChangedDuringWrite)
	}
	if restoring && !expectedSet {
		return Check{}, device.Error(messageRollbackSnapshot)
	}
	if method == "PATCH" {
		if matches(row, values) {
			return Check{Path: path, ID: id, Values: values}, nil
		}
		previous := make(Row, len(values))
		for key := range values {
			if value, ok := original[key]; ok {
				previous[key] = value
			} else {
				previous[key] = ""
			}
		}
		next := MergeRows(row, values)
		*undo = append(*undo, ROSChange{Method: "PATCH", Path: path, ID: id, Values: previous,
			Expected: MarshalRow(d.OwnershipSnapshot(path, next)), Unchanged: MarshalRow(snapshot)})
		if _, err := d.Request(ctx, method, resource, values, Timeout); err != nil {
			return Check{}, err
		}
		return Check{Path: path, ID: id, Values: values}, nil
	}
	if method == "DELETE" {
		if path == PathNAT || path == PathFilter {
			index := IndexOfRow(rows, id)
			if index >= 0 && index+1 < len(rows) {
				original["place-before"] = rows[index+1].ID()
			}
		}
		*undo = append(*undo, ROSChange{Method: "RESTORE", Path: path, ID: id, Values: original,
			Expected: MarshalRow(snapshot), KnownIDs: rowIDList(rows)})
		if _, err := d.Request(ctx, method, resource, nil, Timeout); err != nil {
			return Check{}, err
		}
		return Check{Path: path, ID: id, Absent: true}, nil
	}
	return Check{}, device.Error(fmt.Sprintf(messageUnsupportedWrite, method))
}

func (d *Client) Scoped(ctx context.Context, path string, values Row) ([]Row, error) {
	field := "dst-address"
	if path == PathAddresses || path == PathIPv6AddressLists {
		field = "list"
	}
	query := url.Values{field: []string{values[field]}}.Encode()
	return d.List(ctx, path+"?"+query)
}

func inScope(path string, row, values Row) bool {
	if path == PathAddresses || path == PathIPv6AddressLists {
		return row.Text("list") == values["list"]
	}
	return Comparable("dst-address", row["dst-address"]) == Comparable("dst-address", values["dst-address"]) &&
		device.FirstNonEmpty(row.Text("routing-table"), "main") == device.FirstNonEmpty(values["routing-table"], "main")
}

func hasUnexpectedRoute(rows []Row, values Row, known []string) bool {
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[id] = true
	}
	for _, row := range rows {
		if !knownSet[row.ID()] && row.Text("dst-address") == values["dst-address"] &&
			device.FirstNonEmpty(row.Text("routing-table"), "main") == device.FirstNonEmpty(values["routing-table"], "main") {
			return true
		}
	}
	return false
}

func FindRow(rows []Row, id string) Row {
	for _, row := range rows {
		if row.ID() == id {
			return row
		}
	}
	return nil
}

func IndexOfRow(rows []Row, id string) int {
	for i, row := range rows {
		if row.ID() == id {
			return i
		}
	}
	return -1
}

func RowIDs(rows []Row) map[string]bool {
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		ids[row.ID()] = true
	}
	return ids
}

func rowIDList(rows []Row) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID())
	}
	return ids
}

func MergeRows(left, right Row) Row {
	out := CloneRow(left)
	if out == nil {
		out = Row{}
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func MarshalRow(row Row) json.RawMessage {
	if row == nil {
		return nil
	}
	data, _ := json.Marshal(row)
	return data
}

// SameRow compares two rows; a nil row equals only a nil row.
func SameRow(left, right Row) bool { return (left == nil) == (right == nil) && maps.Equal(left, right) }

func RowsEqual(left Row, raw json.RawMessage) bool {
	right, present := decodeRow(raw)
	return present && maps.Equal(left, right)
}

func (d *Client) VerifyChange(ctx context.Context, check Check) error {
	var row Row
	var rows []Row
	if isSingleRowPath(check.Path) {
		var err error
		row, err = d.Find(ctx, check.Path, check.ID)
		if err != nil {
			return err
		}
		if row != nil {
			rows = []Row{row}
		}
	} else {
		var err error
		rows, err = d.List(ctx, check.Path)
		if err != nil {
			return err
		}
		row = FindRow(rows, check.ID)
	}
	return checkWritten(check, row, rows)
}

func (d *Client) Restore(ctx context.Context, changes []ROSChange) (ROSUndo, error) {
	failures := []string{}
	undo := []ROSChange{}
	for _, change := range changes {
		check, err := d.Perform(ctx, change, &undo, true)
		if err == nil {
			err = d.VerifyChange(ctx, check)
		}
		if err != nil {
			if device.IsRouterError(err) {
				failures = append(failures, err.Error())
			} else {
				return ROSUndo{}, err
			}
		}
	}
	if len(failures) > 0 {
		return ROSUndo{}, device.RollbackError(fmt.Sprintf(messageRollbackIncomplete, strings.Join(failures, "；")))
	}
	slices.Reverse(undo)
	return ROSUndo{Kind: "ros_restore", Changes: undo}, nil
}

func (d *Client) Transaction(ctx context.Context, changes []ROSChange) (ROSUndo, error) {
	undo := []ROSChange{}
	for index := 0; index < len(changes); {
		end := index
		for end < len(changes) && changes[end].Method == "PUT" && changes[end].Path == PathNAT {
			end++
		}
		if end-index >= d.batchMin {
			handled, err := putNAT(ctx, d, changes[index:end], &undo)
			if err != nil {
				return ROSUndo{}, d.compensate(ctx, err, undo)
			}
			index += handled
		}
		for stop := max(end, index+1); index < stop && index < len(changes); index++ {
			check, err := d.Perform(ctx, changes[index], &undo, false)
			if err == nil {
				err = d.VerifyChange(ctx, check)
			}
			if err != nil {
				return ROSUndo{}, d.compensate(ctx, err, undo)
			}
		}
	}
	slices.Reverse(undo)
	return ROSUndo{Kind: "ros_restore", Changes: undo}, nil
}

func (d *Client) compensate(ctx context.Context, cause error, undo []ROSChange) error {
	if len(undo) == 0 {
		return cause
	}
	changes := slices.Clone(undo)
	slices.Reverse(changes)
	_, rollback := d.Restore(lifecycle.Detach(ctx), changes)
	if rollback != nil {
		return device.RollbackError(cause.Error() + "；" + rollback.Error())
	}
	return cause
}
