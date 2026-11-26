package ikuai

// iKuai 4.x driver (backend/routers/ikuai.py).
//
// iKuai has no public API; the driver uses the endpoints of its web UI:
// POST /Action/login, then POST /Action/call with func_name/action/param and
// the session cookie. Only 4.x is supported ({"code": 0, "results": {...}}).
// Write forms mirror the web UI forms, including its string encoding, and are
// always complete: a field left out is reset by iKuai.
//
// Safety rules the driver keeps (Python's, plus the fixes listed in
// parity/PROGRESS-routers.md R05):
//   - a write iKuai answered and refused (a non-zero code) changed nothing:
//     KindRejected, no undo; any other failure of a write (lost reply, empty
//     reply, transport error) may have changed the device: the error carries
//     the undo that finds the change again (RouterError.Undo);
//   - objects ConfigFlow adds carry a unique tagname, so a later undo finds
//     them by name even when no ID was ever read back; an undo deletes only an
//     object whose name and fields still match;
//   - every list is read completely, page by page, and a read that changes
//     between pages or lacks unique IDs fails closed;
//   - an operation, once started, runs to the end even when the caller's
//     request disconnects (PORTING rule 4b); Runtime shutdown still cancels it,
//     and every request has its own timeout.

import (
	"bytes"
	"cmp"
	"configflow/internal/routers/device"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
)

const (
	ikuaiTimeoutSeconds = 10
	ikuaiPageSize       = 1000
	// Writing a rule back on rollback costs iKuai about 1.6 ms per literal entry (4.0.314).
	ikuaiRestoreSlowEntries     = 1000
	ikuaiRestoreSecondsPerEntry = 0.003
	PolicyNexthop               = 1 // 端口分流 type 1: next-hop gateway (0: a WAN line)
)

// IkuaiMaxRequestBytes is the largest request body iKuai 4.0.314 accepts
// with a margin (bodies up to 98 267 bytes were accepted, from 98 308 bytes
// refused with an empty 200 reply).
const IkuaiMaxRequestBytes = 96 * 1024

// Full add/edit forms of the web UI.
var (
	// IkuaiRouteFields is the static_rt form.
	IkuaiRouteFields = []string{"id", "enabled", "tagname", "ip_type", "interface", "dst_addr", "gateway",
		"netmask", "prio", "comment"}
	ikuaiNATFields = []string{"id", "enabled", "tagname", "comment", "action", "protocol", "iinterface",
		"ointerface", "src_addr", "dst_addr", "src_port", "dst_port", "nat_addr", "nat_port", "src_addr_inv",
		"dst_addr_inv"}
	// 端口分流 (stream_ipport) as the 4.0.308 web UI submits it.
	ikuaiPolicyFields = []string{"id", "enabled", "tagname", "interface", "src_addr", "dst_addr", "iface_band",
		"comment", "time", "prio", "mode", "type", "nexthop", "area_code", "src_port", "dst_port", "protocol",
		"dst_type", "src_addr_inv", "dst_addr_inv"}
	// IkuaiDHCPStaticFields is the dhcp_static form (an add also sends hostname "").
	IkuaiDHCPStaticFields = []string{"id", "enabled", "tagname", "interface", "mac", "gateway", "dns1", "dns2",
		"ip_addr", "comment"}
)

// Ikuai is an iKuai 4.x driver. A driver is used by one goroutine at a time
// (the session, the batch snapshot and the proofs of absent adds are per
// driver); create one per release or read.
type Client struct {
	endpoint           string
	username, password string
	client             device.DeviceHTTP
	loggedIn           bool
	batch              *ikuaiBatch
	// absent holds objects this driver added whose absence a complete read
	// later proved ("static_rt\x00<tagname>", "route_object\x00<name>"); their
	// undo then has nothing to remove. Python flipped the "confirmed" flag of
	// the undo dict it had already handed to the release.
	absent map[string]bool
}

// NewIkuai returns a driver for the router's management address.
func NewClient(endpoint, username, password string, verifyTLS bool) *Client {
	return NewClientWithHTTP(endpoint, username, password, devhttp.New(devhttp.Config{VerifyTLS: verifyTLS, Cookies: true}))
}

// NewIkuaiWithHTTP returns a driver using client, which must keep the
// session cookie the login sets.
func NewClientWithHTTP(endpoint, username, password string, client device.DeviceHTTP) *Client {
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), username: username, password: password,
		client: client, absent: map[string]bool{}}
}

// Vendor returns the stored vendor name.
func (*Client) Vendor() string { return "ikuai" }

// SupportsWrite reports that release operations are written directly.
func (*Client) SupportsWrite() bool { return true }

// RenderScript is empty: iKuai is written directly.
func (*Client) RenderScript([]json.RawMessage) (string, error) { return "", nil }

// Close drops idle connections.
func (d *Client) Close() {
	if d.client != nil {
		d.client.Close()
	}
}

// --- transport ---------------------------------------------------------------

func ikuaiSeconds(seconds float64) string { return strconv.FormatFloat(seconds, 'g', 6, 64) }

// post sends one request and returns the reply object. timeout is in seconds
// (0: the default 10).
func (d *Client) post(ctx context.Context, path string, body any, timeout float64) (Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = ikuaiTimeoutSeconds
	}
	target := d.endpoint + path
	response, err := d.client.Do(ctx, http.MethodPost, target, body, time.Duration(timeout*float64(time.Second)))
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		// Only the failure class is shown: the raw error may name the URL.
		var message string
		switch devhttp.ClassOf(err) {
		case devhttp.SSLError:
			message = messageIkuaiTLS
		case devhttp.ConnectTimeout:
			message = fmt.Sprintf(messageIkuaiConnectTimeout, ikuaiTimeoutSeconds)
		case devhttp.ReadTimeout:
			message = fmt.Sprintf(messageIkuaiReadTimeout, ikuaiSeconds(timeout))
		case devhttp.ConnectionError:
			message = messageIkuaiConnection
		default:
			message = messageIkuaiTransport
		}
		return nil, device.Error(message)
	}
	if response.Status != http.StatusOK {
		return nil, ikuaiHTTPError(response, target)
	}
	var data any
	if err := response.JSON(&data); err != nil {
		if len(bytes.Trim(response.Body, " \t\n\r\v\f")) == 0 {
			// The management page is never empty; iKuai drops oversized requests this way.
			return nil, device.Error(messageIkuaiEmptyReply)
		}
		return nil, device.Error(messageIkuaiNotJSON)
	}
	reply, ok := data.(map[string]any)
	if !ok {
		return nil, device.Error(messageIkuaiReplyFormat)
	}
	return reply, nil
}

// ikuaiOrigin is scheme://host[:port] of an absolute http(s) URL, "" for
// anything else: redirects can carry login secrets in the rest of the URL.
func ikuaiOrigin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if (u.Scheme != "http" && u.Scheme != "https") || host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if text := u.Port(); text != "" {
		port, err := strconv.Atoi(text)
		if err != nil || port > 65535 {
			return ""
		}
		if port != map[string]int{"http": 80, "https": 443}[u.Scheme] {
			host += ":" + strconv.Itoa(port)
		}
	}
	return u.Scheme + "://" + host
}

func ikuaiHTTPError(response *devhttp.Response, target string) error {
	code := fmt.Sprintf(messageIkuaiHTTPCode, response.Status)
	switch response.Status {
	case 301, 302, 303, 307, 308:
		base, err := url.Parse(target)
		if err != nil {
			return device.Error(fmt.Sprintf(messageIkuaiRedirect, code))
		}
		redirect := ""
		if location := response.Header.Get("Location"); location != "" {
			ref, err := url.Parse(location)
			if err != nil {
				// Location is untrusted and may contain credentials. Do not wrap
				// url.Parse's error or echo the response header.
				return &device.RouterError{Kind: device.KindInvalidResponse, Message: messageIkuaiRedirectFormat}
			}
			redirect = ikuaiOrigin(base.ResolveReference(ref))
		}
		if redirect != "" && redirect != ikuaiOrigin(base) {
			return device.Error(fmt.Sprintf(messageIkuaiRedirectTarget, redirect, code))
		}
		return device.Error(fmt.Sprintf(messageIkuaiRedirect, code))
	}
	reason, ok := ikuaiHTTPReasons[response.Status]
	if !ok {
		reason = messageIkuaiHTTPOther
		if response.Status >= 500 {
			reason = messageIkuaiHTTPServer
		}
	}
	return device.Error(fmt.Sprintf(messageIkuaiHTTP, reason, code))
}

// --- replies -------------------------------------------------------------------

// jsonInt is a JSON integer (an integer literal; not a bool or a fraction).
func JSONInt(v any) (int64, bool) {
	if n, ok := v.(json.Number); ok {
		i, err := strconv.ParseInt(n.String(), 10, 64)
		return i, err == nil
	}
	return intValue(v)
}

// replyCode is the integer "code" of a reply.
func replyCode(data Row) (int64, bool) { return JSONInt(data["code"]) }

func codeIs(data Row, code int64) bool { n, ok := replyCode(data); return ok && n == code }

// replyText shows a value iKuai sent (strings and numbers as they are).
func ReplyText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case nil:
		return ""
	}
	data, _ := json.Marshal(v)
	return string(data)
}

// nonEmptyText is v when it is a non-empty string or a non-zero number.
func nonEmptyText(v any) string {
	if n, ok := v.(json.Number); ok && n.String() != "0" {
		return n.String()
	}
	s, _ := v.(string)
	return s
}

// ikuaiCallError explains a failed /Action/call. Codes on 4.0.314: 1001 no
// read and 1002 no write permission for the account's user group (Label
// names the feature), 1003 a func_name this firmware lacks, 1008 session
// expired, 3001 invalid action or parameter.
func ikuaiCallError(funcName string, data Row) string {
	message, _ := data["message"].(string)
	label, _ := data["Label"].(string)
	feature := " " + funcName + " "
	if label != "" {
		feature = "「" + label + "」（" + funcName + "）"
	}
	code, isInt := replyCode(data)
	switch {
	case isInt && code == 1001:
		return fmt.Sprintf(messageIkuaiNoRead, feature)
	case isInt && code == 1002:
		return fmt.Sprintf(messageIkuaiNoWrite, feature)
	case isInt && code == 1003:
		detail := ""
		if message != "" {
			detail = "：" + message
		}
		return fmt.Sprintf(messageIkuaiNoFunc, funcName, detail)
	case isInt && code == 1008:
		return messageIkuaiSession
	case data["code"] == nil:
		return fmt.Sprintf(messageIkuaiCallFormat, funcName)
	}
	return fmt.Sprintf(messageIkuaiCallFailed, funcName, cmp.Or(message, messageIkuaiUnknownError), ReplyText(data["code"]))
}

// --- session -------------------------------------------------------------------

func (d *Client) login(ctx context.Context) error {
	if d.loggedIn {
		return nil
	}
	sum := md5.Sum([]byte(d.password))
	data, err := d.post(ctx, "/Action/login", map[string]any{
		"username": d.username,
		"passwd":   hex.EncodeToString(sum[:]),
		"pass":     base64.StdEncoding.EncodeToString([]byte("salt_11" + d.password)),
	}, 0)
	if err != nil {
		return err
	}
	// 4.x answers every login with "Result": 10000, failures included
	// ({"Result":10000,"code":1005,"message":"账号或密码错误"}); only code
	// tells them apart. A reply without code is 3.x style.
	var failed bool
	if _, hasCode := data["code"]; hasCode {
		failed = !codeIs(data, 0)
	} else {
		result, ok := JSONInt(data["Result"])
		failed = !ok || result != 10000
	}
	if failed {
		detail := cmp.Or(nonEmptyText(data["message"]), nonEmptyText(data["ErrMsg"]), messageIkuaiLoginDefault)
		code := ""
		if _, hasCode := data["code"]; hasCode {
			code = fmt.Sprintf(messageIkuaiErrorCode, ReplyText(data["code"]))
		}
		return device.Error(fmt.Sprintf(messageIkuaiLoginFailed, detail, code))
	}
	d.loggedIn = true
	return nil
}

// send posts one /Action/call, logging in first and once more when the
// session expired (code 1008: the router restarted or another login replaced
// it; an expired session ran nothing, so the one retry is safe).
func (d *Client) send(ctx context.Context, payload map[string]any, timeout float64) (Row, error) {
	if err := d.login(ctx); err != nil {
		return nil, err
	}
	data, err := d.post(ctx, "/Action/call", payload, timeout)
	if err != nil || !codeIs(data, 1008) {
		return data, err
	}
	d.loggedIn = false
	if err := d.login(ctx); err != nil {
		return nil, err
	}
	return d.post(ctx, "/Action/call", payload, timeout)
}

// Call runs a show call and returns its results.
func (d *Client) Call(ctx context.Context, funcName string, param map[string]any) (Row, error) {
	data, err := d.send(ctx, map[string]any{"func_name": funcName, "action": "show", "param": param}, 0)
	if err != nil {
		return nil, err
	}
	if results, ok := data["results"].(map[string]any); ok && codeIs(data, 0) {
		return results, nil
	}
	if _, hasCode := data["code"]; !hasCode && (data.Has("Data") || data.Has("Result")) {
		return nil, device.Error(messageIkuai3x)
	}
	return nil, device.Error(ikuaiCallError(funcName, data))
}

// Write submits an add/edit/del form (encoded like the web UI) and returns
// the reply's results. A refusal is a *RouterError of KindRejected: nothing
// changed. Any other error may hide a change (the reply was lost). timeout is
// in seconds (0: the default 10). A started write is not abandoned when ctx
// disconnects (o3-concurrency 2.5); Runtime shutdown still cancels it. The
// device request keeps its own timeout.
func (d *Client) Write(ctx context.Context, funcName, action string, param any, timeout float64) (Row, error) {
	return d.WriteForm(lifecycle.Detach(ctx), funcName, action, param, timeout)
}

// writeForm shares wire encoding and refusal classification. Write and R17
// supply detached Runtime contexts so shutdown can cancel an in-flight write.
func (d *Client) WriteForm(ctx context.Context, funcName, action string, param any, timeout float64) (Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := ikuaiEncodeParam(param)
	if err != nil {
		return nil, err
	}
	data, err := d.send(ctx, map[string]any{"func_name": funcName, "action": action, "param": encoded}, timeout)
	if err != nil {
		return nil, err
	}
	code, ok := replyCode(data)
	if !ok {
		// Python called this a refusal ("… 失败：None"); a reply without a
		// code proves nothing was refused, so it counts as a lost reply.
		return nil, device.Error(fmt.Sprintf(messageIkuaiCallFormat, funcName))
	}
	if code != 0 {
		if slices.Contains([]int64{1001, 1002, 1003, 1008}, code) {
			return nil, &device.RouterError{Kind: device.KindRejected, Message: ikuaiCallError(funcName, data)}
		}
		var details []string
		items, _ := data["details"].([]any)
		for _, item := range items {
			if detail, ok := item.(map[string]any); ok {
				details = append(details, ReplyText(detail["field"])+": "+ReplyText(detail["msg"]))
			}
		}
		message := fmt.Sprintf(messageIkuaiWriteFailed, funcName, action, cmp.Or(nonEmptyText(data["message"]), ReplyText(data["code"])))
		if len(details) > 0 {
			message += "（" + strings.Join(details, "；") + "）"
		}
		return nil, &device.RouterError{Kind: device.KindRejected, Message: message}
	}
	results, _ := data["results"].(map[string]any)
	if results == nil {
		results = Row{}
	}
	return results, nil
}

func isRejected(err error) bool { return device.RouterErrorKind(err) == device.KindRejected }

// ListAll reads every row of a list page by page (prefix "static_" for
// dhcp_static). It fails closed when a page is short, the total changes
// between pages, or (with ids) a row lacks a unique positive integer id.
// incomplete replaces the default incomplete-read message.
func (d *Client) ListAll(ctx context.Context, funcName, prefix string, ids bool, incomplete string) ([]Row, error) {
	label := cmp.Or(ikuaiListLabels[funcName], funcName)
	if incomplete == "" {
		incomplete = fmt.Sprintf(messageIkuaiListIncomplete, label)
	}
	rows := []Row{}
	seen := map[int64]bool{}
	total, offset := int64(-1), int64(0)
	for total < 0 || offset < total {
		result, err := d.Call(ctx, funcName, map[string]any{
			"TYPE":  prefix + "total," + prefix + "data",
			"limit": strconv.FormatInt(offset, 10) + "," + strconv.Itoa(ikuaiPageSize),
		})
		if err != nil {
			return nil, err
		}
		count, countOK := JSONInt(result[prefix+"total"])
		page, pageOK := result[prefix+"data"].([]any)
		if !countOK || count < 0 || !pageOK || (total >= 0 && count != total) ||
			int64(len(page)) != min(ikuaiPageSize, max(0, count-offset)) {
			return nil, device.Error(incomplete)
		}
		total = count
		for _, item := range page {
			row, isRow := item.(map[string]any)
			if !ids {
				if !isRow {
					return nil, device.Error(incomplete)
				}
			} else {
				id, ok := JSONInt(Row(row)["id"])
				if !isRow || !ok || id < 1 || seen[id] {
					return nil, device.Error(fmt.Sprintf(messageIkuaiListIDs, label))
				}
				seen[id] = true
			}
			rows = append(rows, row)
		}
		offset += int64(len(page))
		if total == 0 {
			break
		}
	}
	return rows, nil
}

// --- probe and read-back ---------------------------------------------------------

// Probe checks the firmware (4.x only) and which lists the account may read.
func (d *Client) Probe(ctx context.Context) (*device.RouterInfo, error) {
	results, err := d.Call(ctx, "sysstat", map[string]any{"TYPE": "verinfo"})
	if err != nil {
		return nil, err
	}
	verinfo, ok := results["verinfo"].(map[string]any)
	if !ok && results["verinfo"] != nil {
		return nil, &device.RouterError{Kind: device.KindInvalidResponse, Message: messageIkuaiVersionFormat}
	}
	version := nonEmptyText(Row(verinfo)["version"])
	if !strings.HasPrefix(version, "4.") {
		return nil, device.Error(fmt.Sprintf(messageIkuaiVersion, cmp.Or(version, device.MessageUnknown)))
	}
	capabilities := []string{}
	for _, probe := range [][2]string{{"static_rt", "routes"}, {"nat_rule", "nat_bypass"},
		{"route_object", "address_sets"}, {"stream_ipport", "policy_routes"}} {
		if _, err := d.Call(ctx, probe[0], map[string]any{"TYPE": "total,data", "limit": "0,1"}); err == nil {
			capabilities = append(capabilities, probe[1])
		} else if !device.IsRouterError(err) {
			return nil, err
		}
	}
	return &device.RouterInfo{Vendor: "ikuai", Version: version, Model: nonEmptyText(Row(verinfo)["verstring"]),
		Capabilities: capabilities}, nil
}

// ikuaiRouteMask removes static_rt's presentation suffix, as its edit form
// already does. Only the mask token is configuration; the remaining fields
// still participate in ownership checks without normalization.
func RouteMask(mask string) string {
	mask, _, _ = strings.Cut(mask, " ")
	return mask
}

// ikuaiNetwork is a row's dst_addr/netmask as a network (host bits cleared).
func Network(row Row) (string, bool) {
	network, ok := netipx.ParseNetwork(row.Text("dst_addr")+"/"+RouteMask(row.Text("netmask")), false)
	if !ok {
		return "", false
	}
	return network.String(), true
}

// activeRoutes is 当前路由表: what the kernel forwards with, including routes
// no configuration explains (nil when it cannot be read).
func (d *Client) ActiveRoutes(ctx context.Context) []*device.RouteEntry {
	rows, err := d.ListAll(ctx, "static_rt_table", "", false, "")
	if err != nil {
		return nil
	}
	routes := []*device.RouteEntry{}
	for _, row := range rows {
		if dst, ok := Network(row); ok {
			routes = append(routes, device.NewRoute("active:"+row.Text("id"), dst, row.Text("gateway"), true,
				row.Text("interface")+" prio "+row.Text("prio")))
		}
	}
	return routes
}

// readGroups reads every IP group (decoded, ownership marked; IDs optional).
func (d *Client) ReadGroups(ctx context.Context) ([]*IkuaiGroup, error) {
	rows, err := d.ListAll(ctx, "route_object", "", false, "")
	if err != nil {
		return nil, err
	}
	return GroupsFromRows(DecodeRows(rows)), nil
}

// numberEquals: v is a JSON number equal to n.
func NumberEquals(v any, n int64) bool {
	if number, ok := v.(json.Number); ok {
		f, err := number.Float64()
		return err == nil && f == float64(n)
	}
	i, ok := intValue(v)
	return ok && i == n
}

// ikuaiAddressField is an address/port/time field as the forms send it: iKuai
// returns empty lists as {}, the forms send [].
func ikuaiAddressField(value any) map[string]any {
	field, _ := value.(map[string]any)
	if row, ok := value.(Row); ok {
		field = row
	}
	out := map[string]any{}
	for _, key := range []string{"custom", "object"} {
		list, ok := field[key].([]any)
		if !ok {
			list = []any{}
		}
		out[key] = list
	}
	return out
}

// ikuaiAddresses flattens {"custom": [...], "object": [...]} into addresses. A
// reference resolves by its group ID; only one without an ID by name. A
// reference to a group that no longer exists adds nothing, so a coverage
// check fails closed instead of reading a same-named group.
func Addresses(value any, sets, byGID map[string][]string) []string {
	field, ok := value.(map[string]any)
	if !ok {
		return []string{}
	}
	result := []string{}
	custom, _ := field["custom"].([]any)
	for _, item := range custom {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	objects, _ := field["object"].([]any)
	for _, item := range objects {
		ref, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if gid := Row(ref).Text("gid"); gid != "" {
			result = append(result, byGID[gid]...)
		} else {
			result = append(result, sets[Row(ref).Text("gp_name")]...)
		}
	}
	return result
}

// percentDecode undoes URL percent-encoding of route and NAT comments; an
// invalid escape stays as it is and invalid UTF-8 becomes U+FFFD.
func PercentDecode(text string) string {
	if !strings.Contains(text, "%") {
		return text
	}
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '%' && i+2 < len(text) {
			if b, err := hex.DecodeString(text[i+1 : i+3]); err == nil {
				out = append(out, b[0])
				i += 2
				continue
			}
		}
		out = append(out, text[i])
	}
	return strings.ToValidUTF8(string(out), "�")
}

// --- form values -------------------------------------------------------------------

var ikuaiEscapes = [][2]string{{`"`, "%22"}, {"`", "%60"}, {"$", "%24"}, {"&", "%26"}, {";", "%3B"},
	{`\`, "%5C"}, {"|", "%7C"}, {"'", "%27"}}

// DecodeIkuaiValue undoes the web UI's string encoding in a value iKuai
// returned; key is the field name of a string (comments keep line breaks).
func DecodeIkuaiValue(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = DecodeIkuaiValue(item, k)
		}
		return out
	case Row:
		return Row(DecodeIkuaiValue(map[string]any(v), key).(map[string]any))
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = DecodeIkuaiValue(item, "")
		}
		return out
	case string:
		text := v
		if key == "comment" {
			text = strings.ReplaceAll(strings.ReplaceAll(text, "%0A", "\n"), "%0a", "\n")
		}
		text = strings.ReplaceAll(text, "%20", " ")
		for _, e := range ikuaiEscapes {
			text = strings.ReplaceAll(strings.ReplaceAll(text, e[1], e[0]), strings.ToLower(e[1]), e[0])
		}
		text = strings.ReplaceAll(strings.ReplaceAll(text, "%2F", "/"), "%2f", "/")
		return strings.ReplaceAll(text, "%25", "%")
	}
	return value
}

// decodeRow is a decoded device row.
func DecodeRow(row Row) Row { return DecodeIkuaiValue(row, "").(Row) }

func DecodeRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = DecodeRow(row)
	}
	return out
}

// encodeIkuaiValue encodes every string like the web UI ($("encode", form)).
func encodeIkuaiValue(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = encodeIkuaiValue(item, k)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = encodeIkuaiValue(item, "")
		}
		return out
	case string:
		return ikuaiEncode(v, key == "comment")
	}
	return value
}

// ikuaiEncodeParam is param (any JSON-encodable form) with its strings encoded.
func ikuaiEncodeParam(param any) (any, error) {
	data, err := json.Marshal(param)
	if err != nil {
		return nil, fmt.Errorf("routers: encode iKuai form: %w", err)
	}
	var generic any
	if err := devhttp.DecodeJSON(data, &generic); err != nil {
		return nil, fmt.Errorf("routers: encode iKuai form: %w", err)
	}
	return encodeIkuaiValue(generic, ""), nil
}

// IkuaiRequestBytes is the size of the request a write of param sends,
// counted as Python's json.dumps wrote it (", " and ": " separators): an
// upper bound of the compact body devhttp sends, so the limits measured on a
// device keep their meaning.
func IkuaiRequestBytes(funcName, action string, param any) int {
	encoded, err := ikuaiEncodeParam(param)
	if err != nil {
		return 0
	}
	body, err := devhttp.EncodeJSON(map[string]any{"func_name": funcName, "action": action, "param": encoded})
	if err != nil {
		return 0
	}
	n, inString, escaped := len(body), false, false
	for _, c := range body {
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ',' || c == ':'):
			n++
		}
	}
	return n
}

// IkuaiTagname is the stable 1–15 character name of an object ConfigFlow
// adds, e.g. cf_rt_1a2b3c4d5 (iKuai requires a name and refuses duplicates).
func IkuaiTagname(kind, key string) string {
	sum := sha1.Sum([]byte(key))
	return "cf_" + kind + "_" + hex.EncodeToString(sum[:])[:15-len(kind)-4]
}

// ikuaiRestoreTimeout gives a rule listing many entries literally more time
// to be written back (a rollback of a migration to IP groups); 0 = default.
func ikuaiRestoreTimeout(payload Row) float64 {
	count := 0
	for _, field := range []string{"src_addr", "dst_addr"} {
		count += len(ikuaiAddressField(payload[field])["custom"].([]any))
	}
	if count > ikuaiRestoreSlowEntries {
		return ikuaiTimeoutSeconds + ikuaiRestoreSecondsPerEntry*float64(count)
	}
	return 0
}

// Endpoint returns the configured iKuai base endpoint URL.
func (d *Client) Endpoint() string { return d.endpoint }

// Password returns the configured iKuai password (for redaction only).
func (d *Client) Password() string { return d.password }

// HTTP returns the underlying device HTTP transport.
func (d *Client) HTTP() device.DeviceHTTP { return d.client }

// SetHTTP replaces the underlying device HTTP transport.
func (d *Client) SetHTTP(client device.DeviceHTTP) { d.client = client }
