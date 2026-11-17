package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
)

const (
	rosTimeout        = 10 * time.Second
	rosPingTimeout    = 4 * time.Second
	rosRoutes         = "/ip/route"
	rosIPv6Routes     = "/ipv6/route"
	rosNAT            = "/ip/firewall/nat"
	rosMangle         = "/ip/firewall/mangle"
	rosFilter         = "/ip/firewall/filter"
	rosNetwatch       = "/tool/netwatch"
	rosDHCPNetworks   = "/ip/dhcp-server/network"
	rosAddresses      = "/ip/firewall/address-list"
	rosDHCPServers    = "/ip/dhcp-server"
	rosDHCPLeases     = "/ip/dhcp-server/lease"
	rosDHCPOptions    = "/ip/dhcp-server/option"
	rosDHCPOptionSets = "/ip/dhcp-server/option/sets"
	rosDHCPMatchers   = "/ip/dhcp-server/matcher"
)

var (
	rosRoutePaths     = []string{rosRoutes, rosIPv6Routes}
	rosSingleRowPaths = []string{rosAddresses, rosRoutes, rosIPv6Routes}
	rosRouteFields    = map[string]bool{
		"dst-address": true, "gateway": true, "routing-table": true, "pref-src": true,
		"blackhole": true, "check-gateway": true, "distance": true, "scope": true,
		"target-scope": true, "vrf-interface": true, "suppress-hw-offload": true,
		"disabled": true, "comment": true,
	}
	rosReadOnly = map[string]bool{".id": true, "dynamic": true, "invalid": true, "bytes": true,
		"packets": true, "creation-time": true}
	rosRouteEvidence = map[string]bool{
		"dst-address": true, "gateway": true, "routing-table": true, "pref-src": true,
		"blackhole": true, "check-gateway": true, "distance": true, "scope": true,
		"target-scope": true, "vrf-interface": true, "suppress-hw-offload": true,
		"disabled": true, "comment": true, ".id": true, "immediate-gw": true,
		"active": true, "inactive": true, "connect": true, "ecmp": true, "invalid": true,
	}
	rosIDPattern = regexp.MustCompile(`^\*[0-9a-fA-F]+$`)
)

// DeviceHTTP is the device transport seam. Production uses devhttp; tests use
// a loopback HTTP server and can inject a transport without touching globals.
type DeviceHTTP interface {
	Do(context.Context, string, string, any, time.Duration) (*devhttp.Response, error)
	Close()
}

// BatchDriver is implemented by devices that can apply selected operation
// runs together and restore those undo records in bounded batches.
type BatchDriver interface {
	BatchKinds() []string
	ApplyMany(context.Context, []json.RawMessage, func(json.RawMessage, json.RawMessage)) error
	BatchRestorable(json.RawMessage) bool
	RestoreMany(context.Context, []json.RawMessage, bool) []string
}

// RouterOS is a RouterOS 7 REST device driver. One release or read uses a
// driver from one goroutine at a time (ReadState and the policy/DNS guard
// manifests are per-driver state); only Ping may run concurrently.
type RouterOS struct {
	endpoint string
	client   DeviceHTTP
	// password is only used to hide device echoes of it in manual route errors.
	password string

	// Batch sizes are per driver so independent routers can use their own limits.
	routeBatch int
	batchMin   int

	forwardFilterRows []Row
	natListTouched    bool
	addressCache      map[string][]Row
	addressExpected   map[string][]Row
	addressReads      map[string]addressProof
	policyManifest    json.RawMessage
	dnsGuardManifest  json.RawMessage
}

// NewRouterOS constructs a REST driver with requests-compatible device HTTP.
func NewRouterOS(endpoint, username, password string, verifyTLS bool) *RouterOS {
	driver := NewRouterOSWithHTTP(endpoint, devhttp.New(devhttp.Config{
		VerifyTLS: verifyTLS, BasicAuth: true, Username: username, Password: password,
	}))
	driver.password = password
	return driver
}

// NewRouterOSWithHTTP creates a driver with an injected device client.
func NewRouterOSWithHTTP(endpoint string, client DeviceHTTP) *RouterOS {
	return &RouterOS{endpoint: strings.TrimRight(endpoint, "/"), client: client,
		routeBatch: 100, batchMin: 4}
}

// Vendor returns the stored vendor name.
func (*RouterOS) Vendor() string { return "routeros" }

// SupportsWrite reports that RouterOS REST can apply release operations.
func (*RouterOS) SupportsWrite() bool { return true }

// Close releases idle device connections.
func (d *RouterOS) Close() {
	if d.client != nil {
		d.client.Close()
	}
}

// RenderScript is empty because RouterOS supports direct writes.
func (*RouterOS) RenderScript([]json.RawMessage) (string, error) { return "", nil }

func (d *RouterOS) request(ctx context.Context, method, path string, values Row, timeout time.Duration) (any, error) {
	// Like requests' json=None, no values means no body (a nil Row would be "null").
	var body any
	if values != nil {
		body = values
	}
	response, err := d.client.Do(ctx, method, d.endpoint+"/rest"+path, body, timeout)
	if err != nil {
		kind := KindRouter
		if method != "GET" {
			kind = KindUncertainWrite
		}
		return nil, rosError(kind, fmt.Sprintf(messageROSConnectError, devhttp.ClassOf(err)))
	}
	if response.Status == 401 {
		return nil, routerError(messageROSAuthentication)
	}
	if response.Status == 403 {
		return nil, routerError(messageROSForbidden + permissionHelp)
	}
	if response.Status == 404 && method == "GET" {
		return nil, rosError(KindMissingResource, messageROSRESTUnavailable)
	}
	if response.Status == 404 && method == "DELETE" {
		return Row{}, nil
	}
	if response.Status < 200 || response.Status >= 300 {
		var body Row
		_ = devhttp.DecodeJSON(response.Body, &body)
		detail := d.safeDeviceText(strings.Join(nonEmpty([]string{rosText(body["message"]), rosText(body["detail"])}), ": "))
		kind := KindRouter
		if method != "GET" && response.Status >= 500 {
			kind = KindUncertainWrite
		}
		message := fmt.Sprintf(messageROSHTTPStatus, method, path, response.Status)
		if detail != "" {
			message += "（" + detail + "）"
		}
		if permissionError.MatchString(detail) {
			message = fmt.Sprintf(messageROSPermissionFailure, permissionHelp, message)
		}
		return nil, rosError(kind, message)
	}
	if response.Status == 204 {
		return Row{}, nil
	}
	var result any
	if err := response.JSON(&result); err != nil {
		kind := KindRouter
		if method != "GET" {
			kind = KindUncertainWrite
		}
		return nil, rosError(kind, messageROSNotJSON)
	}
	return result, nil
}

var permissionError = regexp.MustCompile(`(?i)\bnot enough permissions\b`)

func (d *RouterOS) get(ctx context.Context, path string, timeout time.Duration) (any, error) {
	return d.request(ctx, "GET", path, nil, timeout)
}

func (d *RouterOS) list(ctx context.Context, path string) ([]Row, error) {
	value, err := d.get(ctx, path, rosTimeout)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, routerError(fmt.Sprintf(messageROSListFormat, path))
	}
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, routerError(fmt.Sprintf(messageROSListFormat, path))
		}
		rows = append(rows, Row(row))
	}
	if path == rosFilter {
		d.forwardFilterRows = rows
	}
	return rows, nil
}

// Ping reports whether one address answers two RouterOS probes.
func (d *RouterOS) Ping(ctx context.Context, address, iface string, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = rosPingTimeout
	}
	values := Row{"address": address, "count": "2", "interval": "300ms"}
	if iface != "" {
		values["arp-ping"], values["interface"] = "yes", iface
	}
	response, err := d.client.Do(ctx, "POST", d.endpoint+"/rest/ping", values, timeout)
	if err != nil {
		return false, routerError(fmt.Sprintf(messageROSPingIncomplete, devhttp.ClassOf(err)))
	}
	var body any
	if err := response.JSON(&body); err != nil {
		body = nil
	}
	if response.Status == 200 {
		if rows, ok := body.([]any); ok {
			for _, value := range rows {
				row, ok := value.(map[string]any)
				if !ok {
					continue
				}
				received := rosText(row["received"])
				if n, err := strconv.Atoi(received); err == nil && n > 0 {
					return true, nil
				}
			}
			return false, nil
		}
	}
	var detail string
	if row, ok := body.(map[string]any); ok {
		detail = firstText(row["detail"], row["message"])
	}
	if permissionError.MatchString(detail) {
		return false, rosError(KindPingNotPermitted, fmt.Sprintf(messageROSPingPermission, detail))
	}
	message := fmt.Sprintf(messageROSPingHTTPStatus, response.Status)
	if detail != "" {
		message += "（" + detail + "）"
	}
	return false, routerError(message)
}

// Probe checks RouterOS version and which of its read-only surfaces are exposed.
func (d *RouterOS) Probe(ctx context.Context) (*RouterInfo, error) {
	value, err := d.get(ctx, "/system/resource", rosTimeout)
	if err != nil {
		return nil, err
	}
	resource, ok := value.(map[string]any)
	if !ok {
		return nil, routerError(messageROSSystemInfoFormat)
	}
	version := rosText(resource["version"])
	match := versionPattern.FindStringSubmatch(version)
	if len(match) != 3 {
		return nil, routerError(fmt.Sprintf(messageROSVersionRequired, firstNonEmpty(version, messageUnknown)))
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 7 || major == 7 && minor < 1 {
		return nil, routerError(fmt.Sprintf(messageROSVersionRequired, firstNonEmpty(version, messageUnknown)))
	}
	capabilities := []string{}
	for _, item := range []struct{ path, capability string }{
		{rosRoutes, "routes"}, {rosNAT, "nat_bypass"}, {rosAddresses + "?list=cf-probe", "address_sets"},
	} {
		if _, err := d.list(ctx, item.path); err == nil {
			capabilities = append(capabilities, item.capability)
		} else if !isRouterError(err) {
			return nil, err
		}
	}
	return &RouterInfo{Vendor: "routeros", Version: version, Model: rosText(resource["board-name"]),
		Capabilities: capabilities}, nil
}

var versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)`)

// ReadDHCPDNS reads DHCP networks and every RouterOS option layer that can
// replace network option 6. Dynamic leases without custom options are ignored.
func (d *RouterOS) ReadDHCPDNS(ctx context.Context) (*DhcpDnsState, error) {
	paths := []string{rosDHCPNetworks, rosDHCPServers, rosDHCPLeases, rosDHCPOptions, rosDHCPOptionSets, rosDHCPMatchers}
	read := make([][]Row, len(paths))
	for i, path := range paths {
		rows, err := d.list(ctx, path)
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
		if !rosTruthy(server["disabled"]) {
			activeServers = append(activeServers, server)
		}
	}
	if len(activeServers) == 0 {
		globals = append(globals, messageROSDHCPNoActiveServer)
	}
	for _, server := range activeServers {
		label := fmt.Sprintf(messageROSDHCPServiceLabel, firstNonEmpty(firstText(server["name"], server[".id"]), messageUnknown))
		if !server.set("name") || rosTruthy(server["invalid"]) {
			globals = append(globals, fmt.Sprintf(messageROSDHCPIncomplete, label))
		}
		radius := strings.ToLower(rosTextDefault(server["use-radius"], "no"))
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
		if rosTruthy(server["disabled"]) {
			disabledServers[server.text("name")] = true
		}
	}
	for _, matcher := range matchers {
		if rosTruthy(matcher["disabled"]) || disabledServers[matcher.text("server")] {
			continue
		}
		label := fmt.Sprintf(messageROSDHCPMatcherLabel, firstNonEmpty(firstText(matcher["name"], matcher[".id"]), messageUnknown))
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
		if rosTruthy(lease["disabled"]) {
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
		if len(optionNames) > 0 || len(setNames) > 0 || rosTruthy(lease["radius"]) {
			overrideLeases = append(overrideLeases, lease)
		}
	}

	entries := make([]*DhcpDnsEntry, 0, len(networks))
	seenIDs := map[string]bool{}
	for _, row := range networks {
		id := row.id()
		if id == "" || seenIDs[id] {
			return nil, routerError(messageROSDHCPInvalidNetwork)
		}
		seenIDs[id] = true
		network, ok := netipx.ParseNetwork4(row.text("address"), false)
		if !ok {
			return nil, routerError(messageROSDHCPNetworkAddress)
		}
		attributes, err := dhcpNetworkAttributes(row)
		if err != nil {
			return nil, err
		}
		blockers := slices.Clone(globals)
		if rosTruthy(row["dynamic"]) || rosTruthy(row["invalid"]) {
			blockers = append(blockers, messageROSDHCPNetworkBlocked)
		}
		networkLabel := fmt.Sprintf(messageROSDHCPNetworkLabel, network.String())
		networkBlockers, err := dhcpOverrides(row, networkLabel, options, optionSets)
		if err != nil {
			return nil, err
		}
		blockers = append(blockers, networkBlockers...)
		for _, lease := range overrideLeases {
			if disabledServers[lease.text("server")] {
				continue
			}
			address, valid := netipx.ParseAddr4(lease.text("address"))
			if valid && !address.IsUnspecified() && !network.Contains(address) {
				continue
			}
			label := fmt.Sprintf(messageROSDHCPLeaseLabel, firstNonEmpty(firstText(lease["address"], lease[".id"]), messageUnknown))
			if rosTruthy(lease["radius"]) {
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
				return nil, routerError(fmt.Sprintf(messageROSDHCPDNSAddress, network.String()))
			}
			dns = append(dns, address.String())
		}
		if rosTruthy(attributes["dns-none"]) {
			dns = []string{}
		}
		attrs, err := json.Marshal(attributes)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &DhcpDnsEntry{ID: id, Network: network.String(), DNS: dns,
			Enabled: !rosTruthy(row["disabled"]), Attributes: attrs, Blockers: uniqueStrings(blockers), DNSExplicit: true})
	}
	evidence := Row{
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
	return &DhcpDnsState{Entries: entries, Evidence: evidenceJSON}, nil
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
	switch value := attrs["dns-none"].(type) {
	case bool:
	case string:
		if value != "true" && value != "false" && value != "yes" && value != "no" {
			return nil, routerError(messageROSDHCPDNSNone)
		}
	default:
		return nil, routerError(messageROSDHCPDNSNone)
	}
	return attrs, nil
}

func dhcpNames(value any) ([]string, error) {
	var items []string
	switch v := value.(type) {
	case string:
		items = strings.Split(v, ",")
	case []any:
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, routerError(messageROSDHCPNameList)
			}
			items = append(items, text)
		}
	default:
		return nil, routerError(messageROSDHCPNameList)
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
		if len(matches) != 1 || !matches[0].has("options") {
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
		if row.text("name") == name || row.text(".id") == name {
			matches = append(matches, row)
		}
	}
	return matches
}

func optionCode(value any) (int64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case string:
		if v == "vendor-specific" {
			return 43, true
		}
		if v == "" {
			return 0, false
		}
		for _, r := range v {
			if r < '0' || r > '9' {
				return 0, false
			}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
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

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func rosError(kind ErrorKind, message string) error {
	return &RouterError{Kind: kind, Message: message}
}

func isRouterError(err error) bool {
	var target *RouterError
	return errors.As(err, &target)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func rosText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

func rosTextDefault(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	return rosText(value)
}

func firstText(values ...any) string {
	for _, value := range values {
		if text := rosText(value); text != "" {
			return text
		}
	}
	return ""
}

func rosTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	default:
		return false
	}
}

func comparable(key string, value any) string {
	text := rosText(value)
	switch value.(type) {
	case []any, map[string]any:
		// RouterOS sends strings; anything else must still compare by value,
		// never collapse to "" (ownership snapshots rely on it).
		data, _ := json.Marshal(value)
		text = string(data)
	}
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
		if key != "place-before" && comparable(key, row[key]) != comparable(key, value) {
			return false
		}
	}
	return true
}

func referencedAddressLists(rows []Row) []string {
	names := map[string]bool{}
	for _, row := range rows {
		for _, key := range []string{"src-address-list", "dst-address-list"} {
			value, ok := row[key].(string)
			if !ok {
				continue
			}
			name := strings.TrimLeft(value, "!")
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

func selectFields(rows []Row, fields map[string]bool) []Row {
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

func withoutCounters(rows []Row) []Row {
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

// SetPolicyManifest installs the last RouterOS policy manifest for readback
// filtering. The policy-routing module owns the contents of this value.
func (d *RouterOS) SetPolicyManifest(raw json.RawMessage) { d.policyManifest = slices.Clone(raw) }

// SetDNSGuardManifest installs the DNS guard ownership snapshot for NAT
// assessment. The DNS guard module owns the contents of this value.
func (d *RouterOS) SetDNSGuardManifest(raw json.RawMessage) { d.dnsGuardManifest = slices.Clone(raw) }

type rosResourceManifest struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Role   string `json:"role"`
	Config Row    `json:"config"`
}

type rosManifest struct {
	Resources []rosResourceManifest `json:"resources"`
}

func ownedHairpinIDs(raw json.RawMessage, rules []Row) map[string]bool {
	var manifest rosManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return nil
	}
	ids := map[string]bool{}
	for _, entry := range manifest.Resources {
		if !strings.HasPrefix(entry.Role, "hairpin") {
			continue
		}
		for _, row := range rules {
			if row.id() == entry.ID && maps.Equal(stableROSRow(rosNAT, row), entry.Config) {
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

func configROSRow(row Row) Row {
	config := make(Row)
	for key, value := range row {
		if !rosPolicyRuntime[key] {
			config[key] = value
		}
	}
	return config
}

func stableROSRow(path string, row Row) Row {
	fields := map[string]bool{}
	if path == rosNetwatch {
		for _, key := range []string{"name", "host", "port", "type", "interval", "timeout", "startup-delay",
			"up-script", "down-script", "comment", "disabled"} {
			fields[key] = true
		}
	}
	stable := make(Row)
	for key, value := range row {
		if path == rosNetwatch {
			if !fields[key] {
				continue
			}
		} else if strings.HasPrefix(key, ".") || slices.Contains([]string{"dynamic", "invalid", "bytes", "packets", "creation-time"}, key) {
			continue
		}
		stable[key] = comparable(key, value)
	}
	return stable
}

func (d *RouterOS) filterPolicyMangle(state *NatContext) error {
	var manifest rosManifest
	if err := json.Unmarshal(d.policyManifest, &manifest); err != nil {
		return ErrNotPorted
	}
	proven := map[string]bool{}
	for _, entry := range manifest.Resources {
		if entry.Path != rosMangle {
			continue
		}
		for _, row := range state.Mangle {
			if row.id() == entry.ID && sameRow(configROSRow(row), configROSRow(entry.Config)) {
				proven[entry.ID] = true
				break
			}
		}
	}
	state.Mangle = slices.DeleteFunc(state.Mangle, func(row Row) bool { return proven[row.id()] })
	return nil
}

// RegisterRouterOSHandler installs an operation implemented by a later router
// module (policy, NAT lists, DNS guard, or IPv6). The operation body remains
// JSON at the driver boundary and is typed by the owning module.
type RouterOSHandler func(context.Context, *RouterOS, json.RawMessage) (json.RawMessage, error)

var rosHandlerState = struct {
	sync.RWMutex
	byKind map[string]RouterOSHandler
}{byKind: map[string]RouterOSHandler{}}

// RegisterRouterOSHandler registers one later-module operation kind.
func RegisterRouterOSHandler(kind string, handler RouterOSHandler) {
	rosHandlerState.Lock()
	defer rosHandlerState.Unlock()
	if handler == nil {
		delete(rosHandlerState.byKind, kind)
		return
	}
	rosHandlerState.byKind[kind] = handler
}

func routerOSHandler(kind string) RouterOSHandler {
	rosHandlerState.RLock()
	defer rosHandlerState.RUnlock()
	return rosHandlerState.byKind[kind]
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

// ROSUndo is the undo value returned from an applied RouterOS operation.
type ROSUndo struct {
	Kind    string      `json:"kind"`
	Changes []ROSChange `json:"changes"`
}

type rosCheck struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Values Row    `json:"values,omitempty"`
	Absent bool   `json:"absent,omitempty"`
}

type rosScope struct {
	ID      string `json:"id"`
	Network string `json:"network"`
	Gateway string `json:"gateway"`
}

type rosOperation struct {
	Kind           string          `json:"kind"`
	ID             string          `json:"id"`
	CIDR           string          `json:"cidr"`
	Gateway        string          `json:"gateway"`
	Comment        string          `json:"comment"`
	Src            []string        `json:"src"`
	Dst            []string        `json:"dst"`
	AddDst         []string        `json:"add_dst"`
	OutInterface   string          `json:"out_interface"`
	DNS            []string        `json:"dns"`
	Scope          *rosScope       `json:"scope"`
	Expected       json.RawMessage `json:"expected"`
	CheckedDHCPDNS *DhcpDnsState   `json:"checked_dhcp_dns"`
	Changes        []ROSChange     `json:"changes"`
}

// Apply applies one RouterOS release operation and returns its compensation.
// Once started it runs to the end even if ctx is cancelled (PORTING rule 4b):
// a write abandoned between the request and its read-back would leave a
// change on the router that no undo record covers. Every device request
// still has its own timeout.
func (d *RouterOS) Apply(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = context.WithoutCancel(ctx)
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, routerError(messageOperationFormat)
	}
	switch head.Kind {
	case "ros_policy_replace", "ros_policy_restore", "nat_list_sync", "nat_list_restore",
		"ipv6_fakeip_replace", "ipv6_fakeip_restore", "ipv6_routes_replace", "ipv6_routes_restore",
		"ipv6_dns_update", "ipv6_pd_guard_sync", "ipv6_pd_guard_restore", "dns_guard_replace", "dns_guard_restore":
		if handler := routerOSHandler(head.Kind); handler != nil {
			return handler(ctx, d, raw)
		}
		return nil, ErrNotPorted
	}
	var op rosOperation
	if err := json.Unmarshal(raw, &op); err != nil {
		return nil, routerError(messageOperationFormat)
	}
	var result any
	switch op.Kind {
	case "ros_restore":
		undo, err := d.restore(ctx, op.Changes)
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
		result, err = d.transaction(ctx, []ROSChange{{Method: "PUT", Path: rosRoutes, Values: Row{
			"dst-address": op.CIDR, "gateway": op.Gateway, "comment": op.Comment, "disabled": "false",
		}}})
		if err != nil {
			return nil, err
		}
	case "route_delete":
		var err error
		result, err = d.transaction(ctx, []ROSChange{{Method: "DELETE", Path: rosRoutes, ID: op.ID}})
		if err != nil {
			return nil, err
		}
	case "route_comment":
		var err error
		result, err = d.transaction(ctx, []ROSChange{{Method: "PATCH", Path: rosRoutes, ID: op.ID,
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
		return nil, routerError(fmt.Sprintf(messageUnsupportedOperation, op.Kind))
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (d *RouterOS) applyDHCPDNSUpdate(ctx context.Context, op rosOperation) (ROSUndo, error) {
	if len(op.DNS) < 1 || len(op.DNS) > 2 {
		return ROSUndo{}, routerError(messageDHCPDNSCount)
	}
	dns := make([]string, 0, len(op.DNS))
	for _, value := range op.DNS {
		address, ok := netipx.ParseAddr4(value)
		if !ok {
			return ROSUndo{}, routerError(messageDHCPDNSAddressInvalid)
		}
		dns = append(dns, address.String())
	}
	if op.Scope == nil || op.Scope.ID == "" || op.Scope.Network == "" || op.Scope.Gateway == "" || op.Scope.ID != op.ID {
		return ROSUndo{}, routerError(messageDHCPDNSScope)
	}
	checked := op.CheckedDHCPDNS
	if checked == nil {
		var err error
		checked, err = d.ReadDHCPDNS(ctx)
		if err != nil {
			return ROSUndo{}, err
		}
	}
	entry, err := CheckedDNSTarget(DNSCheck{ID: op.Scope.ID, Network: op.Scope.Network, Gateway: op.Scope.Gateway}, checked)
	if err != nil {
		return ROSUndo{}, err
	}
	// The plan always records the previewed attributes; without them there is
	// nothing to prove the network unchanged, so the write is refused.
	expected, present := decodeRow(op.Expected)
	if !present || !sameRow(entry.AttributesRow(), expected) {
		return ROSUndo{}, routerError(messageDHCPDNSDrift)
	}
	return d.transaction(ctx, []ROSChange{{Method: "PATCH", Path: rosDHCPNetworks, ID: entry.ID,
		Values: Row{"dns-server": strings.Join(dns, ","), "dns-none": "false"}}})
}

func (d *RouterOS) applyNATUpdate(ctx context.Context, op rosOperation) (ROSUndo, error) {
	row, err := d.row(ctx, rosNAT, op.ID)
	if err != nil {
		return ROSUndo{}, err
	}
	original := payload(rosNAT, row)
	if original == nil {
		return ROSUndo{}, payloadError(rosNAT, row)
	}
	expected, present := decodeRow(op.Expected)
	if present && !sameRow(original, expected) {
		return ROSUndo{}, routerError(messageNATRuleDrift)
	}
	if !row.is("chain", "srcnat") || !row.is("action", "accept") {
		return ROSUndo{}, routerError(messageNATTargetInvalid)
	}
	changes := []ROSChange{{Method: "PATCH", Path: rosNAT, ID: op.ID, Values: Row{"comment": op.Comment}}}
	seen := map[string]bool{}
	for _, cidr := range op.AddDst {
		if seen[cidr] {
			continue
		}
		seen[cidr] = true
		values := cloneRow(original)
		values["dst-address"], values["comment"], values["place-before"] = cidr, op.Comment, row.id()
		if op.OutInterface != "" {
			values["out-interface"] = op.OutInterface
		}
		changes = append(changes, ROSChange{Method: "PUT", Path: rosNAT, Values: values})
	}
	return d.transaction(ctx, changes)
}

func (d *RouterOS) applyNATAdd(ctx context.Context, op rosOperation) (ROSUndo, error) {
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		return ROSUndo{}, err
	}
	position := Row{}
	if len(rows) > 0 {
		position["place-before"] = rows[0].id()
	}
	sources, err := collapseRouterNetworks(op.Src)
	if err != nil {
		return ROSUndo{}, err
	}
	if len(op.Src) > 0 && len(sources) == 0 {
		return ROSUndo{}, routerError(messageNATSourceInvalid)
	}
	if len(sources) == 0 {
		sources = []string{""}
	}
	changes := []ROSChange{}
	uniqueDst := uniqueStrings(op.Dst)
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
			changes = append(changes, ROSChange{Method: "PUT", Path: rosNAT, Values: values})
		}
	}
	return d.transaction(ctx, changes)
}

func collapseRouterNetworks(values []string) ([]string, error) {
	collapsed, err := netipx.Collapse(Networks(values))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(collapsed))
	for i, network := range collapsed {
		out[i] = network.String()
	}
	return out, nil
}

func (entry *DhcpDnsEntry) AttributesRow() Row {
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

func cloneRow(row Row) Row {
	copy := make(Row, len(row))
	for key, value := range row {
		copy[key] = value
	}
	return copy
}

func isSingleRowPath(path string) bool { return slices.Contains(rosSingleRowPaths, path) }
func isRoutePath(path string) bool     { return slices.Contains(rosRoutePaths, path) }

func (d *RouterOS) find(ctx context.Context, path, id string) (Row, error) {
	if !isSingleRowPath(path) {
		rows, err := d.list(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.id() == id {
				return row, nil
			}
		}
		return nil, nil
	}
	value, err := d.get(ctx, path+"/"+escapeROSPathID(id), rosTimeout)
	if err != nil {
		var rosErr *RouterError
		if errors.As(err, &rosErr) && rosErr.Kind == KindMissingResource {
			return nil, nil
		}
		return nil, err
	}
	row, ok := value.(map[string]any)
	if !ok || rosText(row[".id"]) != id {
		return nil, routerError(fmt.Sprintf(messageROSObjectFormat, path))
	}
	return Row(row), nil
}

func escapeROSPathID(id string) string {
	return strings.ReplaceAll(url.PathEscape(id), "%2A", "*")
}

func (d *RouterOS) row(ctx context.Context, path, id string) (Row, error) {
	row, err := d.find(ctx, path, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, routerError(fmt.Sprintf(messageROSNotFoundAfterManualDelete, path, id))
	}
	return row, nil
}

func payload(path string, row Row) Row {
	if rosTruthy(row["dynamic"]) {
		return nil
	}
	if path == "/ipv6/nd" {
		if rosTruthy(row["invalid"]) {
			return nil
		}
		_ = row["disabled"] // retained in the returned payload below
	}
	if isRoutePath(path) {
		return selectFields([]Row{row}, rosRouteFields)[0]
	}
	if path == "/system/script" {
		return selectFields([]Row{row}, stringSet("name", "source", "policy", "comment", "dont-require-permissions"))[0]
	}
	if path == "/system/scheduler" {
		return selectFields([]Row{row}, stringSet("name", "on-event", "interval", "start-time", "start-date", "policy", "comment", "disabled"))[0]
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
	if path == rosDHCPNetworks {
		attrs, _ := dhcpNetworkAttributes(row)
		return attrs
	}
	if path == rosNetwatch {
		return selectFields([]Row{row}, stringSet("name", "host", "port", "type", "interval", "timeout", "startup-delay", "up-script", "down-script", "comment", "disabled"))[0]
	}
	out := make(Row)
	for key, value := range row {
		if !rosReadOnly[key] && !strings.HasPrefix(key, ".") {
			out[key] = value
		}
	}
	return out
}

// payloadError is why payload refused row: RouterOS-generated rows (and
// invalid IPv6 ND entries) are never taken over, changed or removed.
func payloadError(path string, row Row) error {
	if path == "/ipv6/nd" && !rosTruthy(row["dynamic"]) && rosTruthy(row["invalid"]) {
		return routerError(messageInvalidIPv6ND)
	}
	return routerError(messageROSDynamicObject)
}

func stringSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (d *RouterOS) ownershipSnapshot(path string, row Row) Row {
	values := payload(path, row)
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
			out[key] = comparable(key, value)
		}
	}
	return out
}

func (d *RouterOS) perform(ctx context.Context, change ROSChange, undo *[]ROSChange, restoring bool) (rosCheck, error) {
	method, path := change.Method, change.Path
	values := cloneRow(change.Values)
	if values == nil {
		values = Row{}
	}
	if method == "RESTORE" {
		var current Row
		var rows []Row
		if isSingleRowPath(path) {
			var err error
			current, err = d.find(ctx, path, change.ID)
			if err != nil {
				return rosCheck{}, err
			}
			if isRoutePath(path) && current == nil {
				rows, err = d.scoped(ctx, path, values)
				if err != nil {
					return rosCheck{}, err
				}
			}
		} else {
			var err error
			rows, err = d.list(ctx, path)
			if err != nil {
				return rosCheck{}, err
			}
			current = findRow(rows, change.ID)
		}
		if current != nil {
			if payload(path, current) == nil {
				return rosCheck{}, payloadError(path, current)
			}
			if !rowsEqual(d.ownershipSnapshot(path, current), change.Expected) {
				return rosCheck{}, routerError(messageRollbackChanged)
			}
			delete(values, "place-before")
			return rosCheck{Path: path, ID: change.ID, Values: values}, nil
		}
		if isRoutePath(path) && hasUnexpectedRoute(rows, values, change.KnownIDs) {
			return rosCheck{}, routerError(messageRollbackRouteConflict)
		}
		if values["place-before"] != nil && findRow(rows, rosText(values["place-before"])) == nil {
			return rosCheck{}, routerError(messageRollbackNATOrder)
		}
		return d.perform(ctx, ROSChange{Method: "PUT", Path: path, Values: values}, undo, restoring)
	}

	if method == "PUT" {
		beforeRows := []Row{}
		var err error
		if isSingleRowPath(path) {
			beforeRows, err = d.scoped(ctx, path, values)
		} else {
			beforeRows, err = d.list(ctx, path)
		}
		if err != nil {
			return rosCheck{}, err
		}
		before := rowIDs(beforeRows)
		result, err := d.request(ctx, method, path, values, rosTimeout)
		if err != nil {
			if routerErrorKind(err) == KindUncertainWrite {
				return rosCheck{}, rollbackError(fmt.Sprintf(messageUncertainWrite, err))
			}
			return rosCheck{}, err
		}
		rowMap, ok := result.(map[string]any)
		rowID, idOK := "", false
		if ok {
			rowID, idOK = rowMap[".id"].(string)
		}
		newRow := Row(rowMap)
		if !idOK || !rosIDPattern.MatchString(rowID) || before[rowID] || isSingleRowPath(path) && !inScope(path, newRow, values) {
			return rosCheck{}, rollbackError(fmt.Sprintf(messageUncertainRouteWrite, path))
		}
		expected := d.ownershipSnapshot(path, mergeRows(values, newRow))
		*undo = append(*undo, ROSChange{Method: "DELETE", Path: path, ID: rowID, Expected: marshalRow(expected)})
		return rosCheck{Path: path, ID: rowID, Values: values}, nil
	}

	id := change.ID
	resource := path + "/" + escapeROSPathID(id)
	var row Row
	var rows []Row
	if isSingleRowPath(path) {
		var err error
		row, err = d.find(ctx, path, id)
		if err != nil {
			return rosCheck{}, err
		}
		if row != nil {
			rows = []Row{row}
		}
		if isRoutePath(path) && row != nil && method == "DELETE" {
			neighbors, err := d.scoped(ctx, path, row)
			if err != nil {
				return rosCheck{}, err
			}
			for _, neighbor := range neighbors {
				if neighbor.id() != id {
					rows = append(rows, neighbor)
				}
			}
		}
	} else {
		var err error
		rows, err = d.list(ctx, path)
		if err != nil {
			return rosCheck{}, err
		}
		row = findRow(rows, id)
	}
	if row == nil && method == "DELETE" {
		if restoring {
			current, err := d.get(ctx, resource, rosTimeout)
			if err != nil {
				if routerErrorKind(err) == KindMissingResource {
					return rosCheck{Path: path, ID: id, Absent: true}, nil
				}
				return rosCheck{}, err
			}
			currentMap, ok := current.(map[string]any)
			if !ok || rosText(currentMap[".id"]) != id {
				return rosCheck{}, routerError(messageRollbackUnreadable)
			}
			row = Row(currentMap)
		} else {
			return rosCheck{}, routerError(fmt.Sprintf(messageROSNotFound, path, id))
		}
	}
	if row == nil {
		return rosCheck{}, routerError(fmt.Sprintf(messageROSNotFound, path, id))
	}
	original := payload(path, row)
	if original == nil {
		return rosCheck{}, payloadError(path, row)
	}
	snapshot := d.ownershipSnapshot(path, row)
	unchanged, unchangedSet := decodeRow(change.Unchanged)
	if restoring && unchangedSet && maps.Equal(snapshot, unchanged) {
		return rosCheck{Path: path, ID: id, Values: values}, nil
	}
	expected, expectedSet := decodeRow(change.Expected)
	if expectedSet && !maps.Equal(snapshot, expected) {
		return rosCheck{}, routerError(messageChangedDuringWrite)
	}
	if restoring && !expectedSet {
		return rosCheck{}, routerError(messageRollbackSnapshot)
	}
	if method == "PATCH" {
		if matches(row, values) {
			return rosCheck{Path: path, ID: id, Values: values}, nil
		}
		previous := make(Row, len(values))
		for key := range values {
			if value, ok := original[key]; ok {
				previous[key] = value
			} else {
				previous[key] = ""
			}
		}
		next := mergeRows(row, values)
		*undo = append(*undo, ROSChange{Method: "PATCH", Path: path, ID: id, Values: previous,
			Expected: marshalRow(d.ownershipSnapshot(path, next)), Unchanged: marshalRow(snapshot)})
		if _, err := d.request(ctx, method, resource, values, rosTimeout); err != nil {
			return rosCheck{}, err
		}
		return rosCheck{Path: path, ID: id, Values: values}, nil
	}
	if method == "DELETE" {
		if path == rosNAT || path == rosFilter {
			index := indexOfRow(rows, id)
			if index >= 0 && index+1 < len(rows) {
				original["place-before"] = rows[index+1].id()
			}
		}
		*undo = append(*undo, ROSChange{Method: "RESTORE", Path: path, ID: id, Values: original,
			Expected: marshalRow(snapshot), KnownIDs: rowIDList(rows)})
		if _, err := d.request(ctx, method, resource, nil, rosTimeout); err != nil {
			return rosCheck{}, err
		}
		return rosCheck{Path: path, ID: id, Absent: true}, nil
	}
	return rosCheck{}, routerError(fmt.Sprintf(messageUnsupportedWrite, method))
}

func (d *RouterOS) scoped(ctx context.Context, path string, values Row) ([]Row, error) {
	field := "dst-address"
	if path == rosAddresses {
		field = "list"
	}
	query := url.Values{field: []string{rosText(values[field])}}.Encode()
	return d.list(ctx, path+"?"+query)
}

func inScope(path string, row, values Row) bool {
	if path == rosAddresses {
		return row.text("list") == rosText(values["list"])
	}
	return comparable("dst-address", row["dst-address"]) == comparable("dst-address", values["dst-address"]) &&
		firstNonEmpty(row.text("routing-table"), "main") == firstNonEmpty(rosText(values["routing-table"]), "main")
}

func hasUnexpectedRoute(rows []Row, values Row, known []string) bool {
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[id] = true
	}
	for _, row := range rows {
		if !knownSet[row.id()] && row.text("dst-address") == rosText(values["dst-address"]) &&
			firstNonEmpty(row.text("routing-table"), "main") == firstNonEmpty(rosText(values["routing-table"]), "main") {
			return true
		}
	}
	return false
}

func findRow(rows []Row, id string) Row {
	for _, row := range rows {
		if row.id() == id {
			return row
		}
	}
	return nil
}

func indexOfRow(rows []Row, id string) int {
	for i, row := range rows {
		if row.id() == id {
			return i
		}
	}
	return -1
}

func rowIDs(rows []Row) map[string]bool {
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		ids[row.id()] = true
	}
	return ids
}

func rowIDList(rows []Row) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id())
	}
	return ids
}

func mergeRows(left, right Row) Row {
	out := cloneRow(left)
	if out == nil {
		out = Row{}
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func marshalRow(row Row) json.RawMessage {
	if row == nil {
		return nil
	}
	data, _ := json.Marshal(row)
	return data
}

// sameRow compares two decoded device rows. Values may be JSON arrays or
// objects, which maps.Equal cannot compare without panicking.
func sameRow(left, right Row) bool {
	return len(left) == len(right) && reflect.DeepEqual(map[string]any(left), map[string]any(right))
}

func rowsEqual(left Row, raw json.RawMessage) bool {
	right, present := decodeRow(raw)
	return present && maps.Equal(left, right)
}

func routerErrorKind(err error) ErrorKind {
	var target *RouterError
	if errors.As(err, &target) {
		return target.Kind
	}
	return KindRouter
}

func rollbackError(message string) error { return rosError(KindRollback, message) }

func (d *RouterOS) verifyChange(ctx context.Context, check rosCheck) error {
	var row Row
	var rows []Row
	if isSingleRowPath(check.Path) {
		var err error
		row, err = d.find(ctx, check.Path, check.ID)
		if err != nil {
			return err
		}
		if row != nil {
			rows = []Row{row}
		}
	} else {
		var err error
		rows, err = d.list(ctx, check.Path)
		if err != nil {
			return err
		}
		row = findRow(rows, check.ID)
	}
	return checkWritten(check, row, rows)
}

func (d *RouterOS) restore(ctx context.Context, changes []ROSChange) (ROSUndo, error) {
	failures := []string{}
	undo := []ROSChange{}
	for _, change := range changes {
		check, err := d.perform(ctx, change, &undo, true)
		if err == nil {
			err = d.verifyChange(ctx, check)
		}
		if err != nil {
			if isRouterError(err) {
				failures = append(failures, err.Error())
			} else {
				return ROSUndo{}, err
			}
		}
	}
	if len(failures) > 0 {
		return ROSUndo{}, rollbackError(fmt.Sprintf(messageRollbackIncomplete, strings.Join(failures, "；")))
	}
	slices.Reverse(undo)
	return ROSUndo{Kind: "ros_restore", Changes: undo}, nil
}

func (d *RouterOS) transaction(ctx context.Context, changes []ROSChange) (ROSUndo, error) {
	undo := []ROSChange{}
	for index := 0; index < len(changes); {
		end := index
		for end < len(changes) && changes[end].Method == "PUT" && changes[end].Path == rosNAT {
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
			check, err := d.perform(ctx, changes[index], &undo, false)
			if err == nil {
				err = d.verifyChange(ctx, check)
			}
			if err != nil {
				return ROSUndo{}, d.compensate(ctx, err, undo)
			}
		}
	}
	slices.Reverse(undo)
	return ROSUndo{Kind: "ros_restore", Changes: undo}, nil
}

func (d *RouterOS) compensate(ctx context.Context, cause error, undo []ROSChange) error {
	if len(undo) == 0 {
		return cause
	}
	changes := slices.Clone(undo)
	slices.Reverse(changes)
	_, rollback := d.restore(context.WithoutCancel(ctx), changes)
	if rollback != nil {
		return rollbackError(cause.Error() + "；" + rollback.Error())
	}
	return cause
}

// ReadState returns routes, NAT exemptions, and only the forwarding evidence
// needed to assess the active source-NAT selectors.
func (d *RouterOS) ReadState(ctx context.Context) (*RouterState, error) {
	d.forwardFilterRows = nil
	state := &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{}, AddressSets: map[string][]string{}}
	routes, err := d.list(ctx, rosRoutes)
	if err != nil {
		return nil, err
	}
	nats, err := d.list(ctx, rosNAT)
	if err != nil {
		return nil, err
	}
	mangle, err := d.list(ctx, rosMangle)
	if err != nil && !isRouterError(err) {
		return nil, err
	}
	var mangleRows []Row
	if err == nil {
		mangleRows = mangle
	}
	referencedNames := referencedAddressLists(append(slices.Clone(nats), mangleRows...))
	listNames := slices.Clone(referencedNames)
	if d.natListTouched && !slices.Contains(listNames, "cf-mihomo-nat-src") {
		listNames = append(listNames, "cf-mihomo-nat-src")
	}
	if d.natListTouched && !slices.Contains(listNames, "cf-mihomo-nat-dst") {
		listNames = append(listNames, "cf-mihomo-nat-dst")
	}
	slices.Sort(listNames)
	listRows, err := d.ReadAddresses(ctx, listNames)
	if err != nil {
		return nil, err
	}
	for _, name := range listNames {
		for _, row := range listRows[name] {
			if slices.Contains(referencedNames, name) && !rosTruthy(row["disabled"]) {
				state.AddressSets[name] = append(state.AddressSets[name], row.text("address"))
			}
		}
	}
	if slices.Contains(listNames, NATSrcList) || slices.Contains(listNames, NATDstList) || d.natListTouched {
		state.NATLists, _ = d.stateNATLists(ctx, nats, listRows, mangleRows)
	} else {
		state.NATListsReader = func(ctx context.Context) (*NATLists, error) {
			rows, err := d.ReadAddresses(ctx, []string{NATSrcList, NATDstList})
			if err != nil {
				return nil, err
			}
			return d.stateNATLists(ctx, nats, rows, mangleRows)
		}
	}

	assessed := nats
	if len(d.dnsGuardManifest) != 0 && string(d.dnsGuardManifest) != "null" {
		ids := ownedHairpinIDs(d.dnsGuardManifest, nats)
		assessed = make([]Row, 0, len(nats))
		for _, row := range nats {
			if !ids[row.id()] {
				assessed = append(assessed, row)
			}
		}
	}
	contextRows := &NatContext{
		Routes: selectFields(routes, rosRouteEvidence), Rules: withoutCounters(assessed),
	}
	contextRows.RoutingRules, contextRows.Mangle, contextRows.VRFs = nil, nil, nil
	contextRows.InterfaceLists, contextRows.InterfaceMembers = nil, nil

	scoped := []Row{}
	for _, row := range assessed {
		if row.is("chain", "srcnat") && !rosTruthy(row["disabled"]) &&
			(row.set("out-interface") || row.set("out-interface-list")) {
			scoped = append(scoped, row)
		}
	}
	if len(scoped) > 0 {
		for _, item := range []struct{ field, path string }{
			{"routing_rules", "/routing/rule"}, {"mangle", rosMangle}, {"vrfs", "/ip/vrf"},
		} {
			if item.field == "mangle" && mangleRows != nil {
				contextRows.Mangle = withoutCounters(mangleRows)
				continue
			}
			rows, readErr := d.list(ctx, item.path)
			if readErr != nil {
				if !isRouterError(readErr) {
					return nil, readErr
				}
				continue
			}
			switch item.field {
			case "routing_rules":
				contextRows.RoutingRules = rows
			case "mangle":
				contextRows.Mangle = withoutCounters(rows)
			case "vrfs":
				contextRows.VRFs = rows
			}
		}
		if slices.ContainsFunc(scoped, func(row Row) bool { return row.set("out-interface-list") }) {
			for _, item := range []struct{ field, path string }{
				{"interface_lists", "/interface/list"}, {"interface_members", "/interface/list/member"},
			} {
				rows, readErr := d.list(ctx, item.path)
				if readErr != nil {
					if !isRouterError(readErr) {
						return nil, readErr
					}
					continue
				}
				if item.field == "interface_lists" {
					contextRows.InterfaceLists = rows
				} else {
					contextRows.InterfaceMembers = rows
				}
			}
		}
	}
	state.NatContext = contextRows

	for _, row := range routes {
		dst, ok := netipx.ParseNetwork(row.text("dst-address"), false)
		if !ok {
			continue
		}
		var active *bool
		if row.has("active") {
			active = new(rosTruthy(row["active"]))
		} else if row.has("inactive") {
			active = new(!rosTruthy(row["inactive"]))
		}
		if rosTruthy(row["inactive"]) || rosTruthy(row["invalid"]) {
			active = new(false)
		}
		var distance *int64
		if n, parseErr := strconv.ParseInt(row.text("distance"), 10, 64); parseErr == nil {
			distance = &n
		}
		table := firstNonEmpty(row.text("routing-table"), "main")
		state.Routes = append(state.Routes, &RouteEntry{ID: row.id(), Dst: dst.String(), Gateway: row.text("gateway"),
			Enabled: !rosTruthy(row["disabled"]), Comment: row.text("comment"), Table: table, Active: active, Distance: distance})
	}
	for _, row := range nats {
		comment := row.text("comment")
		if strings.HasPrefix(comment, "cf-ros-policy-") || strings.HasPrefix(comment, "cf-dnsguard:") ||
			!row.is("chain", "srcnat") || !row.is("action", "accept") {
			continue
		}
		dst, src := []string{}, []string{}
		if row.set("dst-address") {
			dst = append(dst, row.text("dst-address"))
		}
		if row.set("dst-address-list") {
			dst = append(dst, state.AddressSets[row.text("dst-address-list")]...)
		}
		if row.set("src-address") {
			src = append(src, row.text("src-address"))
		}
		if row.set("src-address-list") {
			src = append(src, state.AddressSets[row.text("src-address-list")]...)
		}
		attrs := make(Row)
		for key, value := range row {
			if !rosReadOnly[key] && !strings.HasPrefix(key, ".") {
				attrs[key] = value
			}
		}
		encoded, marshalErr := json.Marshal(attrs)
		if marshalErr != nil {
			return nil, marshalErr
		}
		state.NatBypass = append(state.NatBypass, &NatBypassEntry{ID: row.id(), Dst: dst, Src: src,
			Enabled: !rosTruthy(row["disabled"]), Comment: comment, Attributes: encoded})
	}
	if len(d.policyManifest) != 0 {
		if err := d.filterPolicyMangle(contextRows); err != nil && !errors.Is(err, ErrNotPorted) {
			return nil, err
		}
	}
	return state, nil
}
