package routerfake

// A loopback-only iKuai 4.x web API fake for driver boundary tests. It keeps
// what the driver depends on and what was measured on 4.0.314:
//
//   - POST /Action/login checks username, MD5 password and base64
//     "salt_11"+password and sets a session cookie; every login replaces the
//     session; /Action/call without the current session answers code 1008;
//   - show pages with limit "offset,size" and TYPE "<prefix>total,<prefix>data"
//     (dhcp_static uses the static_ prefix); 当前路由表 (static_rt_table)
//     mirrors enabled static routes plus kernel leftovers;
//   - add/edit/del store strings as received (encoded); an edit replaces the
//     configuration fields (a partial form loses fields); fields a form does
//     not have are refused (code 3001), names must be 1–15 characters and
//     unique (3001); IP groups get a group_id IPGP<id>, keep no group comment
//     and report ref_count (one per NAT/端口分流 rule naming the group, plus
//     hidden_refs); a referenced group is not deleted (2021);
//   - request bodies above 98 267 bytes get an empty 200 reply and change
//     nothing;
//   - faults (refusals, HTTP errors, raw bodies, lost replies before or after
//     the change, swallowed writes, corrupted rows) and table mutations on the
//     nth show or write (a hand edit during a release).

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Credentials the fake accepts unless the state names others.
const (
	IkuaiUser     = "synthetic-admin"
	IkuaiPassword = "synthetic-secret"
)

// IkuaiMaxBody is the largest request body the fake (like 4.0.314) accepts.
const IkuaiMaxBody = 98267

// IkuaiCall is one request the fake received.
type IkuaiCall struct {
	Path string `json:"path"`
	Body any    `json:"body"`
}

// IkuaiFault changes the answer to the Count-th request (0: every one)
// matching Func ("login" for /Action/login) and Action ("" any).
type IkuaiFault struct {
	Func   string `json:"func"`
	Action string `json:"action,omitempty"`
	Count  int    `json:"count,omitempty"`
	// Reply with this code (and message/label/details) instead of handling it.
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Label   string `json:"label,omitempty"`
	Details []Row  `json:"details,omitempty"`
	Logout  bool   `json:"logout,omitempty"` // also end the session
	// Reply with an HTTP status, a Location header, a raw body, or a JSON value.
	Status   int     `json:"status,omitempty"`
	Location string  `json:"location,omitempty"` // "{origin}" is replaced by the fake's origin
	RawBody  *string `json:"raw_body,omitempty"`
	Reply    any     `json:"reply,omitempty"`
	// Drop the connection without (Drop) or after (LoseReply) handling it.
	Drop      bool `json:"drop,omitempty"`
	LoseReply bool `json:"lose_reply,omitempty"`
	// Answer code 0 without storing anything.
	Swallow bool `json:"swallow,omitempty"`
	// After an add/edit is stored, also set these fields on the row.
	Corrupt Row `json:"corrupt,omitempty"`
}

// IkuaiMutation changes a table by hand when the Count-th show (OnWrite
// false) or write (OnWrite true, after it is stored) of Func happens.
type IkuaiMutation struct {
	Func    string `json:"func"`
	Action  string `json:"action,omitempty"`
	Count   int    `json:"count"`
	OnWrite bool   `json:"on_write,omitempty"`
	Table   string `json:"table"`
	Pop     bool   `json:"pop,omitempty"`    // remove the last row
	Append  Row    `json:"append,omitempty"` // add a row
	ID      int64  `json:"id,omitempty"`     // with Set: the row to change
	Set     Row    `json:"set,omitempty"`
	Remove  bool   `json:"remove,omitempty"` // with ID: delete that row
}

// IkuaiState is the fake device state and request log.
type IkuaiState struct {
	Tables        map[string][]Row `json:"tables"`
	Stale         []Row            `json:"stale,omitempty"` // kernel routes without a static route
	Version       string           `json:"version,omitempty"`
	Username      string           `json:"username,omitempty"`
	Password      string           `json:"password,omitempty"`
	NoActiveTable bool             `json:"no_active_table,omitempty"`
	Faults        []IkuaiFault     `json:"faults,omitempty"`
	Mutations     []IkuaiMutation  `json:"mutations,omitempty"`
	NextID        int64            `json:"next_id,omitempty"`
	Calls         []IkuaiCall      `json:"calls,omitempty"`
}

// Ikuai is the fake device; control endpoints under /__r05 are not logged.
type Ikuai struct {
	mu      sync.Mutex
	state   IkuaiState
	counts  map[string]int
	session string
	logins  int
}

// NewIkuai returns a fake with the given state.
func NewIkuai(state IkuaiState) *Ikuai {
	f := &Ikuai{}
	f.Reset(state)
	return f
}

var ikuaiTables = []string{"static_rt", "nat_rule", "stream_ipport", "route_object", "dhcp_server", "dhcp_static"}

// Reset replaces the state and clears the log, counters and session.
func (f *Ikuai) Reset(state IkuaiState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = cloneIkuai(state)
	if f.state.Tables == nil {
		f.state.Tables = map[string][]Row{}
	}
	for _, name := range ikuaiTables {
		if f.state.Tables[name] == nil {
			f.state.Tables[name] = []Row{}
		}
	}
	if f.state.Version == "" {
		f.state.Version = "4.0.314"
	}
	if f.state.Username == "" && f.state.Password == "" {
		f.state.Username, f.state.Password = IkuaiUser, IkuaiPassword
	}
	if f.state.NextID == 0 {
		f.state.NextID = 100
		for _, rows := range f.state.Tables {
			for _, row := range rows {
				if id, ok := rowID(row); ok && id >= f.state.NextID {
					f.state.NextID = id + 1
				}
			}
		}
	}
	f.state.Calls = nil
	f.counts = map[string]int{}
	f.session = ""
	f.logins = 0
}

// Snapshot is a deep copy of the state, including the request log.
func (f *Ikuai) Snapshot() IkuaiState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneIkuai(f.state)
}

// Logins counts successful logins since the last reset.
func (f *Ikuai) Logins() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins
}

func cloneIkuai(state IkuaiState) IkuaiState {
	data, _ := json.Marshal(state)
	var out IkuaiState
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return out
}

func rowID(row Row) (int64, bool) {
	switch v := row["id"].(type) {
	case json.Number:
		id, err := strconv.ParseInt(v.String(), 10, 64)
		return id, err == nil
	case float64:
		return int64(v), v == float64(int64(v))
	case int:
		return int64(v), true
	case int64:
		return v, true
	case string:
		id, err := strconv.ParseInt(v, 10, 64)
		return id, err == nil
	}
	return 0, false
}

// ServeHTTP serves the iKuai web API and the /__r05 controls.
func (f *Ikuai) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	if strings.HasPrefix(r.URL.Path, "/__r05/") {
		f.control(w, r, data)
		return
	}
	var body any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	_ = dec.Decode(&body)
	f.mu.Lock()
	f.state.Calls = append(f.state.Calls, IkuaiCall{Path: r.URL.Path, Body: body})
	if len(data) > IkuaiMaxBody {
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	request, _ := body.(map[string]any)
	funcName, action := "login", ""
	if r.URL.Path == "/Action/call" {
		funcName, _ = request["func_name"].(string)
		action, _ = request["action"].(string)
	} else if r.URL.Path != "/Action/login" {
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	fault := f.fault(funcName, action)
	if fault != nil && fault.Drop {
		f.mu.Unlock()
		closeConnection(w)
		return
	}
	if fault != nil && (fault.Status != 0 || fault.RawBody != nil || fault.Reply != nil || fault.Code != nil) {
		if fault.Logout {
			f.session = ""
		}
		f.mu.Unlock()
		f.faultReply(w, r, fault)
		return
	}
	var status int
	var reply any
	var cookie string
	if funcName == "login" {
		reply, cookie = f.login(request)
		status = http.StatusOK
	} else {
		status, reply = f.call(r, funcName, action, request, fault)
	}
	f.mu.Unlock()
	if fault != nil && fault.LoseReply {
		closeConnection(w)
		return
	}
	if cookie != "" {
		http.SetCookie(w, &http.Cookie{Name: "sess_key", Value: cookie, Path: "/"})
	}
	writeJSON(w, status, reply)
}

func (f *Ikuai) faultReply(w http.ResponseWriter, r *http.Request, fault *IkuaiFault) {
	if fault.Location != "" {
		// {origin} is the fake's own scheme://host:port.
		w.Header().Set("Location", strings.ReplaceAll(fault.Location, "{origin}", "http://"+r.Host))
	}
	status := fault.Status
	if status == 0 {
		status = http.StatusOK
	}
	switch {
	case fault.RawBody != nil:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, *fault.RawBody)
	case fault.Reply != nil:
		writeJSON(w, status, fault.Reply)
	case fault.Code != nil:
		reply := map[string]any{"Result": 10000, "code": *fault.Code, "message": fault.Message}
		if fault.Label != "" {
			reply["Label"] = fault.Label
		}
		if fault.Details != nil {
			reply["details"] = fault.Details
		}
		writeJSON(w, status, reply)
	default:
		writeJSON(w, status, map[string]any{"message": "synthetic failure"})
	}
}

// fault is the fault for this request (counted per func/action), if any.
func (f *Ikuai) fault(funcName, action string) *IkuaiFault {
	key := funcName + "/" + action
	f.counts[key]++
	f.counts[funcName+"/"]++
	for i := range f.state.Faults {
		fault := &f.state.Faults[i]
		if fault.Func != funcName || (fault.Action != "" && fault.Action != action) {
			continue
		}
		n := f.counts[key]
		if fault.Action == "" {
			n = f.counts[funcName+"/"]
		}
		if fault.Count == 0 || fault.Count == n {
			return fault
		}
	}
	return nil
}

func (f *Ikuai) login(request map[string]any) (any, string) {
	sum := md5.Sum([]byte(f.state.Password))
	if request["username"] != f.state.Username || request["passwd"] != hex.EncodeToString(sum[:]) ||
		request["pass"] != base64.StdEncoding.EncodeToString([]byte("salt_11"+f.state.Password)) {
		return map[string]any{"Result": 10000, "code": 1005, "message": "账号或密码错误"}, ""
	}
	f.logins++
	f.session = "s" + strconv.Itoa(f.logins)
	return map[string]any{"Result": 10000, "code": 0, "message": "成功"}, f.session
}

func refused(code int, message string, details ...Row) map[string]any {
	reply := map[string]any{"Result": 10000, "code": code, "message": message}
	if len(details) > 0 {
		reply["details"] = details
	}
	return reply
}

// Fields each add/edit form may carry.
var ikuaiForms = map[string][]string{
	"static_rt": {"id", "enabled", "tagname", "ip_type", "interface", "dst_addr", "gateway", "netmask", "prio", "comment"},
	"nat_rule": {"id", "enabled", "tagname", "comment", "action", "protocol", "iinterface", "ointerface", "src_addr",
		"dst_addr", "src_port", "dst_port", "nat_addr", "nat_port", "src_addr_inv", "dst_addr_inv"},
	"stream_ipport": {"id", "enabled", "tagname", "interface", "src_addr", "dst_addr", "iface_band", "comment", "time",
		"prio", "mode", "type", "nexthop", "area_code", "src_port", "dst_port", "protocol", "dst_type", "src_addr_inv",
		"dst_addr_inv"},
	"route_object": {"id", "group_name", "type", "group_value", "comment"},
	"dhcp_static":  {"id", "enabled", "tagname", "interface", "mac", "gateway", "dns1", "dns2", "ip_addr", "comment", "hostname"},
}

func init() {
	fields := []string{"id", "enabled", "tagname", "interface", "phy_ifnames", "addr_pool", "exclude_pool", "netmask",
		"gateway", "next_server", "dns1", "dns2", "domain", "wins1", "wins2", "lease", "delay", "check_addr_valid",
		"check_relay_only"}
	for _, n := range []int{15, 28, 43, 60, 66, 67, 80, 119, 125, 128, 138, 121} {
		fields = append(fields, "opt_type"+strconv.Itoa(n), "opt"+strconv.Itoa(n))
	}
	ikuaiForms["dhcp_server"] = fields
}

// Runtime fields an edit keeps (the forms do not carry them).
var ikuaiRuntime = map[string][]string{
	"route_object": {"group_id", "hidden_refs"},
	"dhcp_server":  {"status", "available"},
	"dhcp_static":  {"termname", "hostname", "ip_addr_int"},
}

func (f *Ikuai) call(r *http.Request, funcName, action string, request map[string]any, fault *IkuaiFault) (int, any) {
	if cookie, err := r.Cookie("sess_key"); err != nil || f.session == "" || cookie.Value != f.session {
		return http.StatusOK, refused(1008, "登录超时，请重新登录")
	}
	param, _ := request["param"].(map[string]any)
	if action == "show" {
		f.mutate(funcName, "", false)
		return http.StatusOK, f.show(funcName, param)
	}
	table, ok := f.state.Tables[funcName]
	if !ok || funcName == "dhcp_server" && action != "edit" {
		return http.StatusOK, refused(1003, "Access denied")
	}
	if action != "add" && action != "edit" && action != "del" {
		return http.StatusOK, refused(3001, "请求参数不合法")
	}
	if reply := f.validate(funcName, action, param, table); reply != nil {
		return http.StatusOK, reply
	}
	if fault != nil && fault.Swallow {
		return http.StatusOK, map[string]any{"Result": 10000, "code": 0, "message": "成功", "results": map[string]any{}}
	}
	switch action {
	case "add":
		row := Row{"enabled": "yes"}
		maps.Copy(row, param)
		row["id"] = f.state.NextID
		if funcName == "route_object" {
			delete(row, "comment")
			row["tagname"] = row["group_name"]
			row["group_id"] = "IPGP" + strconv.FormatInt(f.state.NextID, 10)
		}
		f.state.NextID++
		if fault != nil && fault.Corrupt != nil {
			maps.Copy(row, fault.Corrupt)
		}
		f.state.Tables[funcName] = append(table, row)
	case "edit":
		id, _ := rowID(param)
		i := slices.IndexFunc(table, func(row Row) bool { other, _ := rowID(row); return other == id })
		row := Row{}
		for _, key := range ikuaiRuntime[funcName] {
			if value, ok := table[i][key]; ok {
				row[key] = value
			}
		}
		maps.Copy(row, param)
		row["id"] = table[i]["id"]
		if funcName == "route_object" {
			delete(row, "comment")
			row["tagname"] = row["group_name"]
		}
		if fault != nil && fault.Corrupt != nil {
			maps.Copy(row, fault.Corrupt)
		}
		table[i] = row
	case "del":
		id, _ := rowID(param)
		f.state.Tables[funcName] = slices.DeleteFunc(table, func(row Row) bool { other, _ := rowID(row); return other == id })
	}
	f.mutate(funcName, action, true)
	return http.StatusOK, map[string]any{"Result": 10000, "code": 0, "message": "成功", "results": map[string]any{}}
}

// validate refuses what 4.0.314 refuses: unknown fields, missing or duplicate
// names, an add with an id, an edit or delete of a missing row, deleting a
// referenced IP group.
func (f *Ikuai) validate(funcName, action string, param map[string]any, table []Row) any {
	if param == nil {
		return refused(3001, "请求参数不合法")
	}
	id, hasID := rowID(param)
	index := slices.IndexFunc(table, func(row Row) bool { other, _ := rowID(row); return hasID && other == id })
	if action == "del" {
		if index < 0 {
			return refused(3001, "请求参数不合法", Row{"field": "id", "msg": "记录不存在"})
		}
		if funcName == "route_object" && f.refCount(table[index]) > 0 {
			return refused(2021, "该资源正在使用")
		}
		return nil
	}
	for key := range param {
		if !slices.Contains(ikuaiForms[funcName], key) {
			return refused(3001, "请求参数不合法", Row{"field": key, "msg": "不支持的参数"})
		}
	}
	if action == "add" && hasID {
		return refused(3001, "请求参数不合法", Row{"field": "id", "msg": "不支持的参数"})
	}
	if action == "edit" && index < 0 {
		return refused(3001, "请求参数不合法", Row{"field": "id", "msg": "记录不存在"})
	}
	if funcName == "dhcp_server" {
		for _, key := range ikuaiForms[funcName] {
			if _, ok := param[key]; !ok {
				return refused(3001, "请求参数不合法", Row{"field": key, "msg": "缺少参数"})
			}
		}
		return nil
	}
	nameKey := "tagname"
	if funcName == "route_object" {
		nameKey = "group_name"
	}
	name, _ := param[nameKey].(string)
	if funcName != "dhcp_static" && (len([]rune(name)) < 1 || len([]rune(name)) > 15) {
		return refused(2, "请求参数不合法", Row{"field": nameKey, "msg": "名称长度为1-15"})
	}
	for i, row := range table {
		if i != index && name != "" && row[nameKey] == name {
			return refused(3001, "请求参数不合法", Row{"field": nameKey, "msg": "名称已存在，请重新输入"})
		}
	}
	return nil
}

// refCount is ref_count: one per NAT/端口分流 rule naming the group in either
// address field (enabled or not), plus hidden_refs.
func (f *Ikuai) refCount(group Row) int64 {
	count, _ := rowID(Row{"id": group["hidden_refs"]})
	names := func(row Row) bool {
		for _, field := range []string{"src_addr", "dst_addr"} {
			value, _ := row[field].(map[string]any)
			objects, _ := value["object"].([]any)
			if slices.ContainsFunc(objects, func(item any) bool {
				ref, ok := item.(map[string]any)
				return ok && ref["gid"] == group["group_id"]
			}) {
				return true
			}
		}
		return false
	}
	for _, funcName := range []string{"nat_rule", "stream_ipport"} {
		for _, row := range f.state.Tables[funcName] {
			if names(row) {
				count++
			}
		}
	}
	return count
}

func (f *Ikuai) show(funcName string, param map[string]any) any {
	typ, _ := param["TYPE"].(string)
	if funcName == "sysstat" {
		return map[string]any{"Result": 10000, "code": 0, "message": "成功", "results": map[string]any{
			"verinfo": map[string]any{"version": f.state.Version, "verstring": f.state.Version + " x64 Build202601010000"}}}
	}
	var rows []Row
	switch {
	case funcName == "static_rt_table" && !f.state.NoActiveTable:
		for _, row := range f.state.Tables["static_rt"] {
			if enabled, _ := row["enabled"].(string); enabled == "yes" {
				rows = append(rows, Row{"id": row["id"], "dst_addr": row["dst_addr"], "netmask": row["netmask"],
					"gateway": row["gateway"], "interface": "lan1", "prio": row["prio"]})
			}
		}
		rows = append(rows, f.state.Stale...)
	default:
		table, ok := f.state.Tables[funcName]
		if !ok {
			return refused(1003, "Access denied")
		}
		for _, row := range table {
			shown := maps.Clone(row)
			if funcName == "route_object" {
				delete(shown, "hidden_refs")
				shown["ref_count"] = f.refCount(row)
			}
			rows = append(rows, shown)
		}
	}
	prefix := ""
	if strings.HasPrefix(typ, "static_") {
		prefix = "static_"
	}
	offset, size := 0, len(rows)
	if limit, ok := param["limit"].(string); ok {
		first, second, _ := strings.Cut(limit, ",")
		offset, _ = strconv.Atoi(first)
		size, _ = strconv.Atoi(second)
	}
	page := []Row{}
	if offset < len(rows) {
		page = rows[offset:min(len(rows), offset+size)]
	}
	return map[string]any{"Result": 10000, "code": 0, "message": "成功",
		"results": map[string]any{prefix + "total": len(rows), prefix + "data": page}}
}

// mutate applies the mutations due at this show (action "") or write.
func (f *Ikuai) mutate(funcName, action string, onWrite bool) {
	key := fmt.Sprintf("mutation/%s/%s/%v", funcName, action, onWrite)
	f.counts[key]++
	for _, m := range f.state.Mutations {
		if m.Func != funcName || m.OnWrite != onWrite || (onWrite && m.Action != "" && m.Action != action) {
			continue
		}
		count := f.counts[key]
		if onWrite && m.Action == "" {
			f.counts[fmt.Sprintf("mutation/%s/any", funcName)]++
			count = f.counts[fmt.Sprintf("mutation/%s/any", funcName)]
		}
		if m.Count != count {
			continue
		}
		table := f.state.Tables[m.Table]
		if m.Table == "stale" {
			table = f.state.Stale
		}
		switch {
		case m.Pop && len(table) > 0:
			table = table[:len(table)-1]
		case m.Append != nil:
			table = append(table, maps.Clone(m.Append))
		case m.Remove:
			table = slices.DeleteFunc(table, func(row Row) bool { id, _ := rowID(row); return id == m.ID })
		case m.Set != nil:
			for _, row := range table {
				if id, _ := rowID(row); id == m.ID {
					maps.Copy(row, m.Set)
				}
			}
		}
		if m.Table == "stale" {
			f.state.Stale = table
		} else {
			f.state.Tables[m.Table] = table
		}
	}
}

func (f *Ikuai) control(w http.ResponseWriter, r *http.Request, data []byte) {
	switch r.URL.Path {
	case "/__r05/reset":
		var state IkuaiState
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.Reset(state)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "/__r05/state":
		writeJSON(w, http.StatusOK, f.Snapshot())
	default:
		http.NotFound(w, r)
	}
}
