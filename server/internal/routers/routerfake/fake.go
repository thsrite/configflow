// Package routerfake provides a loopback-only RouterOS REST server for driver
// boundary tests. It records the wire requests and supports the REST operations
// used by the RouterOS driver, including /execute console adds.
package routerfake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Row is one fake RouterOS object.
type Row map[string]any

// Call is a request received by the fake device.
type Call struct {
	Method string     `json:"method"`
	Path   string     `json:"path"`
	Query  url.Values `json:"query,omitempty"`
	Body   any        `json:"body,omitempty"`
}

// ConsoleAdd is one /execute add command parsed from its script.
type ConsoleAdd struct {
	Index      int    `json:"index"`
	Collection string `json:"collection"`
	Values     Row    `json:"values"`
}

// Fault injects one response failure. Count selects the nth request with the
// same Method and Path; zero selects every matching request.
type Fault struct {
	Method       string           `json:"method"`
	Path         string           `json:"path"`
	Count        int              `json:"count,omitempty"`
	Status       int              `json:"status,omitempty"`
	Body         Row              `json:"body,omitempty"`
	RawBody      string           `json:"raw_body,omitempty"`
	OutputSuffix []string         `json:"output_suffix,omitempty"`
	ExtraRows    map[string][]Row `json:"extra_rows,omitempty"`
	LoseReply    bool             `json:"lose_reply,omitempty"`
	DelayMS      int              `json:"delay_ms,omitempty"` // delay the reply after accepting the operation
	AfterCommit  bool             `json:"after_commit,omitempty"`
	// PatchCreated changes fields of the row an /execute add created, by the
	// add's index, as a device that normalizes or flags a new row would.
	PatchCreated map[int]Row `json:"patch_created,omitempty"`
}

// State is the fake device state and request log.
type State struct {
	Tables        map[string][]Row `json:"tables"`
	Calls         []Call           `json:"calls"`
	Faults        []Fault          `json:"faults,omitempty"`
	Mutations     []Mutation       `json:"mutations,omitempty"`
	HideNewRoutes bool             `json:"hide_new_routes,omitempty"`
}

// Fake implements a synthetic RouterOS REST device. Control endpoints under
// /__r04 are excluded from the recorded device request log.
type Fake struct {
	mu            sync.Mutex
	tables        map[string][]Row
	calls         []Call
	faults        []Fault
	mutations     []Mutation
	counts        map[string]int
	nextID        int
	hiddenIDs     map[string]bool
	hideNewRoutes bool
}

// New returns an empty stateful RouterOS fake with all R04 tables available.
func New(tables map[string][]Row) *Fake {
	f := &Fake{}
	f.Reset(State{Tables: tables})
	return f
}

// Reset replaces the device tables and clears request counts and logs.
func (f *Fake) Reset(state State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables = cloneTables(state.Tables)
	for _, path := range []string{
		"/ip/route", "/ipv6/route", "/ip/firewall/nat", "/ip/firewall/mangle", "/ip/firewall/filter",
		"/ip/firewall/address-list", "/routing/rule", "/ip/vrf", "/interface/list", "/interface/list/member",
		"/ip/dhcp-server/network", "/ip/dhcp-server", "/ip/dhcp-server/lease", "/ip/dhcp-server/option",
		"/ip/dhcp-server/option/sets", "/ip/dhcp-server/matcher", "/tool/netwatch", "/system/script",
		"/system/scheduler", "/ipv6/dhcp-client", "/ipv6/nd",
	} {
		if _, ok := f.tables[path]; !ok {
			f.tables[path] = []Row{}
		}
	}
	f.calls = nil
	f.faults = slices.Clone(state.Faults)
	f.mutations = slices.Clone(state.Mutations)
	f.counts = map[string]int{}
	f.nextID = 100
	f.hiddenIDs = map[string]bool{}
	f.hideNewRoutes = state.HideNewRoutes
}

// Snapshot returns a deep copy of tables and calls.
func (f *Fake) Snapshot() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return State{Tables: cloneTables(f.tables), Calls: cloneCalls(f.calls), Faults: slices.Clone(f.faults),
		HideNewRoutes: f.hideNewRoutes}
}

// Calls returns the device requests recorded so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneCalls(f.calls)
}

// ServeHTTP serves the synthetic RouterOS REST API and private test controls.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/__r04/") {
		f.control(w, r)
		return
	}
	var body any
	if r.Body != nil {
		data, _ := io.ReadAll(r.Body)
		if len(bytes.TrimSpace(data)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			_ = dec.Decode(&body)
		}
	}
	path := strings.TrimPrefix(r.URL.Path, "/rest")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	query := r.URL.Query()
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: r.Method, Path: path, Query: query, Body: body})
	fault := f.requestFault(r.Method, path)
	if fault != nil && !fault.AfterCommit {
		f.mu.Unlock()
		if fault.LoseReply {
			closeConnection(w)
			return
		}
		writeFault(w, *fault)
		return
	}
	if r.Method == http.MethodGet {
		f.mutateLocked(r.Method, path)
	}
	status, result, closeReply := f.serveLocked(r.Method, path, query, body, fault)
	if r.Method != http.MethodGet {
		f.mutateLocked(r.Method, path)
	}
	f.mu.Unlock()
	if closeReply || fault != nil && fault.LoseReply {
		closeConnection(w)
		return
	}
	if fault != nil && fault.AfterCommit && (fault.Body != nil || fault.RawBody != "" || fault.Status != 0) {
		if fault.DelayMS > 0 {
			time.Sleep(time.Duration(fault.DelayMS) * time.Millisecond)
		}
		writeFault(w, *fault)
		return
	}
	if fault != nil && fault.DelayMS > 0 {
		time.Sleep(time.Duration(fault.DelayMS) * time.Millisecond)
	}
	writeJSON(w, status, result)
}

func (f *Fake) control(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/__r04/reset":
		var state State
		_ = json.NewDecoder(r.Body).Decode(&state)
		f.Reset(state)
		writeJSON(w, http.StatusOK, Row{"ok": true})
	case "/__r04/state":
		writeJSON(w, http.StatusOK, f.Snapshot())
	default:
		writeJSON(w, http.StatusNotFound, Row{"detail": "not found"})
	}
}

func (f *Fake) requestFault(method, path string) *Fault {
	key := strings.ToUpper(method) + " " + path
	f.counts[key]++
	count := f.counts[key]
	for i := range f.faults {
		fault := &f.faults[i]
		if strings.EqualFold(fault.Method, method) && fault.Path == path && (fault.Count == 0 || fault.Count == count) {
			return fault
		}
	}
	return nil
}

func (fault Fault) status() int {
	if fault.Status == 0 {
		return http.StatusBadRequest
	}
	return fault.Status
}

func (fault Fault) body() Row {
	if fault.Body != nil {
		return fault.Body
	}
	return Row{"error": fault.status(), "message": "Bad Request", "detail": "injected write failure"}
}

func writeFault(w http.ResponseWriter, fault Fault) {
	if fault.RawBody != "" {
		w.WriteHeader(fault.status())
		_, _ = io.WriteString(w, fault.RawBody)
		return
	}
	writeJSON(w, fault.status(), fault.body())
}

func (f *Fake) serveLocked(method, path string, query url.Values, body any, fault *Fault) (int, any, bool) {
	if path == "/system/resource" && method == http.MethodGet {
		return http.StatusOK, Row{"version": "7.24.5 (stable)", "board-name": "CHR"}, false
	}
	if path == "/ping" && method == http.MethodPost {
		return http.StatusOK, []Row{{"received": "1"}}, false
	}
	if method == http.MethodPost && path == "/execute" {
		response, lost := f.execute(body, fault)
		return http.StatusOK, response, lost
	}
	if method == http.MethodPost && strings.HasSuffix(path, "/print") {
		collection := strings.TrimSuffix(path, "/print")
		rows, ok := f.tables[collection]
		if !ok {
			return http.StatusNotFound, Row{"message": "Not Found"}, false
		}
		return http.StatusOK, f.printRows(rows, body), false
	}
	if method == http.MethodPost && strings.HasSuffix(path, "/remove") {
		collection := strings.TrimSuffix(path, "/remove")
		rowBody := asRow(body)
		ids := strings.Split(text(rowBody[".id"]), ",")
		for _, id := range ids {
			if fault := f.requestFault("DELETE", collection+"/"+id); fault != nil {
				return fault.status(), fault.body(), false
			}
			rows, ok := f.tables[collection]
			if !ok {
				return http.StatusNotFound, Row{"message": "Not Found"}, false
			}
			index := rowIndex(rows, id)
			if index < 0 {
				return http.StatusNotFound, Row{"error": 404, "message": "Not Found"}, false
			}
			f.tables[collection] = slices.Delete(rows, index, index+1)
			delete(f.hiddenIDs, id)
		}
		return http.StatusOK, []Row{}, false
	}

	collection, id := f.splitResource(path)
	rows, ok := f.tables[collection]
	if !ok {
		return http.StatusNotFound, Row{"message": "Not Found"}, false
	}
	if method == http.MethodGet {
		if id != "" {
			index := rowIndex(rows, id)
			if index < 0 {
				return http.StatusNotFound, Row{"message": "Not Found"}, false
			}
			return http.StatusOK, cloneRow(rows[index]), false
		}
		visible := make([]Row, 0, len(rows))
		for _, row := range rows {
			if !f.hiddenIDs[text(row[".id"])] && matchesQuery(row, query) {
				visible = append(visible, cloneRow(row))
			}
		}
		return http.StatusOK, visible, false
	}
	if method == http.MethodPut {
		if id != "" {
			return http.StatusNotFound, Row{"message": "Not Found"}, false
		}
		newRow := asRow(body)
		if newRow == nil {
			return http.StatusBadRequest, Row{"message": "Bad Request", "detail": "expected object"}, false
		}
		created, err := f.add(collection, newRow)
		if err != nil {
			return errorStatus(err), Row{"message": "Bad Request", "detail": err.Error()}, false
		}
		return http.StatusCreated, created, fault != nil && fault.AfterCommit && fault.LoseReply
	}
	if method == http.MethodPatch {
		if id == "" {
			return http.StatusNotFound, Row{"message": "Not Found"}, false
		}
		index := rowIndex(rows, id)
		if index < 0 {
			return http.StatusNotFound, Row{"error": 404, "message": "Not Found"}, false
		}
		for key, value := range asRow(body) {
			rows[index][key] = value
		}
		f.tables[collection] = rows
		return http.StatusOK, cloneRow(rows[index]), fault != nil && fault.AfterCommit && fault.LoseReply
	}
	if method == http.MethodDelete {
		if id == "" {
			return http.StatusNotFound, Row{"message": "Not Found"}, false
		}
		index := rowIndex(rows, id)
		if index < 0 {
			return http.StatusNotFound, Row{"error": 404, "message": "Not Found"}, false
		}
		f.tables[collection] = slices.Delete(rows, index, index+1)
		delete(f.hiddenIDs, id)
		return http.StatusNoContent, nil, fault != nil && fault.AfterCommit && fault.LoseReply
	}
	return http.StatusMethodNotAllowed, Row{"message": "Method Not Allowed"}, false
}

func (f *Fake) splitResource(path string) (string, string) {
	best := ""
	for collection := range f.tables {
		if (path == collection || strings.HasPrefix(path, collection+"/")) && len(collection) > len(best) {
			best = collection
		}
	}
	if best == "" {
		return "", ""
	}
	if path == best {
		return best, ""
	}
	return best, strings.TrimPrefix(path, best+"/")
}

func (f *Fake) add(collection string, values Row) (Row, error) {
	f.nextID++
	id := fmt.Sprintf("*%X", f.nextID)
	row := Row{".id": id, "disabled": "false"}
	for key, value := range values {
		row[key] = value
	}
	before := text(row["place-before"])
	delete(row, "place-before")
	rows := f.tables[collection]
	if before != "" {
		index := rowIndex(rows, before)
		if index < 0 {
			return nil, fmt.Errorf("RouterOS %s 中找不到 place-before=%s", collection, before)
		}
		rows = slices.Insert(rows, index, row)
	} else {
		rows = append(rows, row)
	}
	f.tables[collection] = rows
	if f.hideNewRoutes && (collection == "/ip/route" || collection == "/ipv6/route") {
		f.hiddenIDs[id] = true
	}
	return cloneRow(row), nil
}

func (f *Fake) execute(body any, fault *Fault) (Row, bool) {
	script := text(asRow(body)["script"])
	lines := strings.Split(script, "\n")
	output := []string{}
	for _, line := range lines {
		index, command, ok := parseAddLine(line)
		if !ok {
			continue
		}
		collection, values, ok := parseAddCommand(command)
		if !ok {
			output = append(output, fmt.Sprintf("cf-err:%d:malformed add", index))
			break
		}
		writeFault := f.requestFault("PUT", collection)
		if writeFault != nil && !writeFault.AfterCommit {
			output = append(output, fmt.Sprintf("cf-err:%d:%v", index, writeFault.body()["detail"]))
			break
		}
		created, err := f.add(collection, values)
		if err != nil {
			output = append(output, fmt.Sprintf("cf-err:%d:%s", index, err.Error()))
			break
		}
		if fault != nil && fault.PatchCreated[index] != nil {
			rows := f.tables[collection]
			if at := rowIndex(rows, text(created[".id"])); at >= 0 {
				for key, value := range fault.PatchCreated[index] {
					rows[at][key] = value
				}
			}
		}
		output = append(output, fmt.Sprintf("cf-id:%d:%s", index, text(created[".id"])))
		if writeFault != nil && writeFault.AfterCommit && writeFault.LoseReply {
			return Row{}, true
		}
	}
	if fault != nil {
		for path, rows := range fault.ExtraRows {
			for _, row := range rows {
				f.tables[path] = append(f.tables[path], cloneRow(row))
			}
		}
		output = append(output, fault.OutputSuffix...)
	}
	output = append(output, "cf-done")
	return Row{"ret": strings.Join(output, "\n")}, fault != nil && fault.AfterCommit && fault.LoseReply
}

func (f *Fake) printRows(rows []Row, body any) []Row {
	query := asRow(body)[".query"]
	terms := stringsFrom(query)
	filters := []string{}
	for _, term := range terms {
		if term != "#" && !strings.HasPrefix(term, "#") {
			filters = append(filters, term)
		}
	}
	filtered := []Row{}
	for _, row := range rows {
		if len(filters) == 0 || matchesTerms(row, filters) {
			if !f.hiddenIDs[text(row[".id"])] {
				filtered = append(filtered, cloneRow(row))
			}
		}
	}
	return filtered
}

func matchesQuery(row Row, query url.Values) bool {
	for key, values := range query {
		for _, value := range values {
			if text(row[key]) != value {
				return false
			}
		}
	}
	return true
}

func matchesTerms(row Row, filters []string) bool {
	for _, filter := range filters {
		key, value, ok := strings.Cut(filter, "=")
		if ok && text(row[key]) == value {
			return true
		}
	}
	return false
}

var addLinePattern = regexp.MustCompile(`^:if \(\$f=0\) do=\{:onerror e in=\{:put \("cf-id:(\d+):" \. \[(.*)\]\)\} do=\{:set f 1; :put \("cf-err:\d+:" \. \$e\)\}\}$`)

func parseAddLine(line string) (int, string, bool) {
	match := addLinePattern.FindStringSubmatch(line)
	if len(match) != 3 {
		return 0, "", false
	}
	index, err := strconv.Atoi(match[1])
	return index, match[2], err == nil
}

func parseAddCommand(command string) (string, Row, bool) {
	parts, ok := splitConsole(command)
	if !ok || len(parts) < 1 {
		return "", nil, false
	}
	resource, rest := parts[0], parts[1:]
	if !strings.HasSuffix(resource, "/add") {
		return "", nil, false
	}
	collection := strings.TrimSuffix(resource, "/add")
	values := Row{}
	for _, part := range rest {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return "", nil, false
		}
		if value == "yes" && (key == "disabled" || key == "log") {
			value = "true"
		} else if value == "no" && (key == "disabled" || key == "log") {
			value = "false"
		}
		values[key] = value
	}
	return collection, values, true
}

// ParseConsoleAdds parses the /add commands used by the R04 driver's console
// batches, so boundary tests compare fields and order rather than script text.
func ParseConsoleAdds(script string) ([]ConsoleAdd, error) {
	adds := []ConsoleAdd{}
	for _, line := range strings.Split(script, "\n") {
		index, command, ok := parseAddLine(line)
		if !ok {
			continue
		}
		collection, values, ok := parseAddCommand(command)
		if !ok {
			return nil, fmt.Errorf("could not parse RouterOS console add")
		}
		adds = append(adds, ConsoleAdd{Index: index, Collection: collection, Values: values})
	}
	return adds, nil
}

func splitConsole(text string) ([]string, bool) {
	parts := []string{}
	var token strings.Builder
	inQuote, escaped, started := false, false, false
	for _, r := range text {
		if escaped {
			switch r {
			case 'n':
				token.WriteByte('\n')
			case 'r':
				token.WriteByte('\r')
			case 't':
				token.WriteByte('\t')
			default:
				token.WriteRune(r)
			}
			escaped, started = false, true
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			started = true
			continue
		}
		if !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r') {
			if started {
				parts = append(parts, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteRune(r)
		started = true
	}
	if inQuote || escaped {
		return nil, false
	}
	if started {
		parts = append(parts, token.String())
	}
	return parts, true
}

func cloneTables(tables map[string][]Row) map[string][]Row {
	out := make(map[string][]Row, len(tables))
	for path, rows := range tables {
		copyRows := make([]Row, len(rows))
		for i, row := range rows {
			copyRows[i] = cloneRow(row)
		}
		out[path] = copyRows
	}
	return out
}

func cloneCalls(calls []Call) []Call {
	out := make([]Call, len(calls))
	for i, call := range calls {
		query := url.Values{}
		for key, values := range call.Query {
			query[key] = slices.Clone(values)
		}
		out[i] = Call{Method: call.Method, Path: call.Path, Query: query, Body: cloneValue(call.Body)}
	}
	return out
}

func cloneRow(row Row) Row {
	if row == nil {
		return nil
	}
	out := Row{}
	for key, value := range row {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case Row:
		return cloneRow(v)
	case map[string]any:
		return cloneRow(Row(v))
	case []Row:
		out := make([]Row, len(v))
		for i, row := range v {
			out[i] = cloneRow(row)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func rowIndex(rows []Row, id string) int {
	for index, row := range rows {
		if text(row[".id"]) == id {
			return index
		}
	}
	return -1
}

func asRow(value any) Row {
	if row, ok := value.(map[string]any); ok {
		return Row(row)
	}
	if row, ok := value.(Row); ok {
		return row
	}
	return nil
}

func stringsFrom(value any) []string {
	out := []string{}
	switch v := value.(type) {
	case []string:
		return slices.Clone(v)
	case []any:
		for _, item := range v {
			out = append(out, text(item))
		}
	}
	return out
}

func text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func errorStatus(err error) int { return http.StatusBadRequest }

func writeJSON(w http.ResponseWriter, status int, value any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func closeConnection(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		conn, _, err := h.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}
}

var _ http.Handler = (*Fake)(nil)
