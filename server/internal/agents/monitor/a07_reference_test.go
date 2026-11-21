package monitor

// A07 reference corpus from CPython 3.11 (testdata/gen_a07_reference.py →
// testdata/a07_reference.json.gz): mihomo_panel.call, oxidns_cache.call and
// oxidns_rule_update.call on edge inputs and Agent answers (outcome and the
// request that reached the Agent), _newer_release, the version.txt rule,
// last_complete_slot_end, and a ranking history scenario replayed operation
// by operation with every stored row compared.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func loadA07(t *testing.T, fields ...string) map[string]any {
	t.Helper()
	return referenceFields(t, "testdata/a07_reference.json.gz", fields...)
}

// outcomeOf is the corpus' outcome of a call: {"ok": result}, the
// MonitorError fields, or {"exception": ...} for any other error.
func outcomeOf(result any, err error) map[string]any {
	if err == nil {
		return map[string]any{"ok": result}
	}
	var flushErr *CacheOperationError
	if errors.As(err, &flushErr) {
		flushed := flushErr.Flushed
		if flushed == nil {
			flushed = []string{}
		}
		return map[string]any{
			"error": flushErr.Err.Code, "status": flushErr.Err.Status,
			"message": flushErr.Err.Message, "flushed": flushed,
		}
	}
	var e *Error
	if errors.As(err, &e) {
		return map[string]any{"error": e.Code, "status": e.Status, "message": e.Message}
	}
	return map[string]any{"exception": ""}
}

func caseClient(c map[string]any) *replyClient {
	reply := asList(c["reply"])
	client := &replyClient{}
	if reply[0] == "raise" {
		client.add(errors.New("refused"))
	} else {
		body, _ := base64.StdEncoding.DecodeString(reply[1].(string))
		status := asInt64(reply[0])
		client.add(&agents.Response{StatusCode: int(status), Body: body})
	}
	return client
}

func lowerOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.ToLower(u.Scheme+"://"+u.Host) + strings.TrimPrefix(raw, u.Scheme+"://"+u.Host)
}

// seenOf is the corpus' record of the requests that reached the Agent.
func seenOf(client *replyClient) []any {
	out := []any{}
	for _, c := range client.calls {
		r := c.req
		var data any
		if r.Body != nil {
			data = requestData(string(r.Body))
		}
		headers := map[string]any{}
		for _, key := range []string{"Authorization", "Accept", "Content-Type"} {
			if value := r.Header.Get(key); value != "" {
				headers[key] = value
			}
		}
		out = append(out, map[string]any{
			"method": r.Method, "url": lowerOrigin(r.URL), "data": data, "headers": headers,
			"timeout":         []any{int64(r.Timeout.Connect.Seconds()), int64(r.Timeout.Read.Seconds())},
			"trust_env":       !r.NoProxy,
			"allow_redirects": !r.NoRedirects,
			"stream":          r.MaxBody > 0,
		})
	}
	return out
}

// requestData compares JSON request bodies by value: the Agent parses them,
// so Python's json.dumps spacing and ASCII escapes are not part of the wire
// contract. Non-JSON bodies stay text.
func requestData(body string) any {
	if json.Valid([]byte(body)) {
		return json.RawMessage(body)
	}
	return body
}

func normalizeSeen(t *testing.T, text string) any {
	t.Helper()
	value := parseJSONVal(t, []byte(text))
	for _, item := range asList(value) {
		o := asMap(item)
		o["url"] = lowerOrigin(asStr(o["url"]))
		if data, ok := o["data"].(string); ok {
			o["data"] = requestData(data)
		}
	}
	parsed, err := testjson.Parse(mustMarshal(t, value))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func caseManager(t *testing.T, c map[string]any, client *replyClient) (*fakeManager, string) {
	agentMap := asMap(c["agent"])
	id := asStr(agentMap["id"])
	return &fakeManager{agents: map[string]*model.Agent{id: mustAgentMap(agentMap)}, dataDir: t.TempDir(), client: client}, id
}

// nonLatin1Token reports cases whose token http.client could not encode:
// the corpus' patched Session.request never reaches http.client, so Python
// answered them; the real requests stack raises UnicodeEncodeError
// (AGENT_UNREACHABLE), as the Flask differential confirms.
func nonLatin1Token(c map[string]any) bool {
	token, ok := asMap(c["agent"])["token"].(string)
	if !ok {
		return false
	}
	for _, r := range token {
		if r > 0xff {
			return true
		}
	}
	return false
}

func compareCall(t *testing.T, label string, c map[string]any, result any, err error, client *replyClient) bool {
	t.Helper()
	if nonLatin1Token(c) {
		wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
		return false
	}
	// N8-11: machine query numbers are ASCII. Keep the frozen Python rows as
	// evidence and assert the approved refusal before comparing other rows.
	if numericQueryChange(c) {
		wantMonitorError(t, err, "INVALID_REQUEST", 400)
		if len(client.calls) != 0 {
			t.Fatal("invalid numeric query reached device")
		}
		return false
	}
	got := outcomeOf(result, err)
	gotVal, perr := testjson.Parse(mustMarshal(t, got))
	if perr != nil {
		t.Fatal(perr)
	}
	if nativePanelExpectation(t, c, gotVal) {
		if diff := diffRefVal(t, seenOf(client), normalizeSeen(t, asStr(c["seen"]))); diff != "" {
			t.Fatalf("native refusal request changed: %s", diff)
		}
		return false
	}
	// N8-11: CPython's 4300-digit int_max_str_digits cap is retired; native
	// CallPanel preserves valid 4301-digit RFC 8259 integer literals intact.
	if reply := asList(c["reply"]); len(reply) == 2 && asInt64(reply[0]) == 200 {
		if decoded, dErr := base64.StdEncoding.DecodeString(asStr(reply[1])); dErr == nil && string(decoded) == `{"n": `+strings.Repeat("1", 4301)+`}` {
			origWant := parseJSONMap(t, []byte(asStr(c["outcome"])))
			if asStr(origWant["error"]) != "INVALID_RESPONSE" || asInt64(origWant["status"]) != 502 {
				t.Fatalf("%s: 4301-digit row original expectation drifted: %s", label, asStr(c["outcome"]))
			}
			want := `{"ok":{"success":true,"data":{"n":` + strings.Repeat("1", 4301) + `}}}`
			if diff := diffRefJSON(t, got, want); diff != "" {
				t.Errorf("%s native 4301-digit reply: %s", label, diff)
			}
			if diff := diffRefVal(t, seenOf(client), normalizeSeen(t, asStr(c["seen"]))); diff != "" {
				t.Errorf("%s seen: %s", label, diff)
			}
			return true
		}
	}
	wantMap := parseJSONMap(t, []byte(asStr(c["outcome"])))
	if _, ok := wantMap["exception"]; ok {
		if _, raised := got["exception"]; !raised {
			t.Errorf("%s: Python raised %s, Go %v", label, asStr(c["outcome"]), got)
		}
	} else if diff := diffRefJSON(t, got, asStr(c["outcome"])); diff != "" {
		t.Errorf("%s: %s", label, diff)
	}
	if diff := diffRefVal(t, seenOf(client), normalizeSeen(t, asStr(c["seen"]))); diff != "" {
		t.Errorf("%s seen: %s", label, diff)
	}
	return false
}

func pairsOf(list []any) [][2]string {
	out := [][2]string{}
	for _, item := range list {
		pair := item.([]any)
		out = append(out, [2]string{pair[0].(string), pair[1].(string)})
	}
	return out
}

func stringSliceOf(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func callCacheCorpus(ctx context.Context, m Registry, id, method string, value any) (*CacheResult, error) {
	var tags []string
	if value != nil {
		if list, ok := value.([]any); ok {
			for _, item := range list {
				if _, isList := item.([]any); isList {
					return nil, errors.New("cache tags must be strings")
				}
				if _, isMap := item.(map[string]any); isMap {
					return nil, errors.New("cache tags must be strings")
				}
			}
		}
		var ok bool
		tags, ok = stringSliceOf(value)
		if !ok {
			return nil, CacheFail("INVALID_REQUEST", 400)
		}
	}
	return CallCache(ctx, m, id, method, tags)
}

func callRuleUpdateCorpus(ctx context.Context, m Registry, id, method string, value any) (any, int, error) {
	var paths []string
	if value != nil {
		var ok bool
		paths, ok = stringSliceOf(value)
		if !ok {
			return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400)
		}
	}
	result, status, err := CallRuleUpdate(ctx, m, id, method, paths)
	if err != nil {
		return nil, status, err
	}
	if result.Pair != nil {
		return json.RawMessage(result.Pair), status, nil
	}
	return result, status, nil
}

func TestReferencePanelCalls(t *testing.T) {
	ref := loadA07(t, "panel")
	matched4301 := 0
	for i, item := range asList(ref["panel"]) {
		c := asMap(item)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		var body json.RawMessage
		if c["body"] != nil {
			body = mustMarshal(t, c["body"])
		}
		result, err := CallPanel(ctx, m, id, asStr(c["resource"]), asStr(c["method"]), pairsOf(asList(c["args"])), body, "")
		if compareCall(t, fmt.Sprintf("i=%d resource=%v args=%v", i, c["resource"], c["args"]), c, result, err, client) {
			matched4301++
		}
	}
	if matched4301 != 1 {
		t.Fatalf("expected 1 native 4301-digit panel row, matched %d", matched4301)
	}
}

func TestReferenceCacheCalls(t *testing.T) {
	ref := loadA07(t, "cache")
	for i, item := range asList(ref["cache"]) {
		c := asMap(item)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		result, err := callCacheCorpus(ctx, m, id, asStr(c["method"]), c["tags"])
		compareCall(t, fmt.Sprintf("i=%d tags=%v reply=%v", i, c["tags"], c["reply"]), c, result, err, client)
	}
}

func TestReferenceRuleUpdateCalls(t *testing.T) {
	ref := loadA07(t, "rule_update")
	for i, item := range asList(ref["rule_update"]) {
		c := asMap(item)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		data, status, err := callRuleUpdateCorpus(ctx, m, id, asStr(c["method"]), c["paths"])
		var result any
		if err == nil {
			result = []any{data, status}
		}
		compareCall(t, fmt.Sprintf("i=%d paths_len=%d reply=%v", i, len(asList(c["paths"])), c["reply"]), c, result, err, client)
	}
}

func TestReferencePanelHelpers(t *testing.T) {
	ref := loadA07(t, "core_version_texts", "last_complete_slot_end", "newer_release")
	for _, item := range asList(ref["newer_release"]) {
		c := item.([]any)
		raw := mustMarshal(t, c)
		if native, bound := nativeVersionExpected(t, "newer_release", raw); bound {
			got, err := newerRelease(c[0].(string), c[1].(string))
			if err != nil || got != native["result"] {
				t.Errorf("native release: %v %v want %v", got, err, native)
			}
			continue
		}
		if got, err := newerRelease(c[0].(string), c[1].(string)); err != nil || got != c[2].(bool) {
			t.Errorf("newer_release(%q, %q) = %v", c[0], c[1], got)
		}
	}
	for _, item := range asList(ref["core_version_texts"]) {
		c := item.([]any)
		raw, _ := base64.StdEncoding.DecodeString(c[0].(string))
		version := trimText(agents.DecodeUTF8Replace(raw))
		var got any
		if coreVersionPattern.MatchString(version) {
			got = version
		}
		if got != c[1] {
			t.Errorf("version.txt %q: %v want %v", raw, got, c[1])
		}
	}
	for _, item := range asList(ref["last_complete_slot_end"]) {
		c := item.([]any)
		n := asInt64(c[0])
		want := asInt64(c[1])
		if got := LastCompleteSlotEnd(n); got != want {
			t.Errorf("last_complete_slot_end(%d) = %d want %d", n, got, want)
		}
	}
}

// --- ranking scenario ---------------------------------------------------------------

func slotFromCorpus(o map[string]any) *Slot {
	slot := &Slot{Records: asInt64(o["records"]), Partial: o["partial"] == true, Rows: map[string]*Counts{}}
	if blocked, ok := asInt64OK(o["blocked"]); ok && o["blocked"] != nil {
		slot.Blocked = &blocked
	}
	rows := asMap(o["rows"])
	for kind, value := range rows {
		counts := &Counts{Values: map[string]int64{}}
		for _, pair := range value.([]any) {
			p := pair.([]any)
			counts.Add(p[0].(string), asInt64(p[1]))
		}
		slot.Rows[kind] = counts
	}
	return slot
}

func answerJSON(a *HistoryAnswer) map[string]any {
	rows := map[string]any{}
	for _, kind := range RankingKinds {
		list := []any{}
		for _, row := range a.Rows[kind] {
			list = append(list, []any{row.Key, row.Count})
		}
		rows[kind] = list
	}
	var first, last any
	if a.Slots != 0 {
		first, last = a.First, a.Last
	}
	return map[string]any{
		"records": a.Records, "blocked_records": a.BlockedRecords, "blocked_slots": a.BlockedSlots,
		"slots": a.Slots, "partial_slots": a.PartialSlots, "first": first, "last": last, "rows": rows,
	}
}

func sqlValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case nil:
		return nil
	}
	return v
}

// dbDump is the generator's db_dump with Go's driver.
func dbDump(t *testing.T, path string) map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	rowsOf := func(statement string) []any {
		rows, err := db.Query(statement)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, _ := rows.Columns()
		out := []any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			row := make([]any, len(values))
			for i, v := range values {
				row[i] = sqlValue(v)
			}
			out = append(out, row)
		}
		return out
	}
	out := map[string]any{
		"journal_mode": mode,
		"master":       rowsOf("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name"),
	}
	for _, table := range []string{"ranking_periods", "ranking_rows", "ranking_slots"} {
		out[table] = rowsOf("SELECT rowid, * FROM " + table + " ORDER BY rowid")
	}
	db.Close()
	header := make([]byte, 100)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Read(header)
	f.Close()
	out["header_18_20"] = []any{int64(header[18]), int64(header[19])}
	entries, _ := os.ReadDir(filepath.Dir(path))
	files := []string{}
	for _, e := range entries {
		files = append(files, e.Name())
	}
	sort.Strings(files)
	list := make([]any, len(files))
	for i, f := range files {
		list[i] = f
	}
	out["files"] = list
	return out
}

func TestReferenceRankingScenario(t *testing.T) {
	ref := asMap(loadA07(t, "ranking")["ranking"])
	ops, results := asList(ref["ops"]), asList(ref["results"])
	store := NewRankingStore(t.TempDir())
	for i, item := range ops {
		op := asMap(item)
		var got any
		switch asStr(op["op"]) {
		case "collect":
			var records []rankRecord
			for _, r := range asList(op["records"]) {
				f := r.([]any)
				records = append(records, rankRecord{asInt64(f[0]), f[1].(string), f[2].(string), f[3] == true})
			}
			useAgent(t, newFakeAgent(records).do)
			agentID := asStr(op["agent"])
			stored, err := CollectAgentHistory(ctx, nil, store, mustAgentMap(map[string]any{"id": agentID, "name": agentID, "version": HistoryAgentVersion}),
				[]string{asStr(op["recorder"])}, i64(asInt64(op["now"])), int(asInt64(op["budget"])), int(asInt64(op["retention"])), 0)
			if err != nil {
				t.Fatal(err)
			}
			var caught any
			if end, ok := store.CaughtUp(agentID); ok {
				caught = end
			}
			got = []any{int64(stored), caught}
		case "add":
			added, err := store.AddSlot(asStr(op["agent"]), asStr(op["recorder"]), asInt64(op["start"]), slotFromCorpus(asMap(op["slot"])))
			if err != nil {
				t.Fatal(err)
			}
			got = added
		case "query":
			answer, err := store.Query(asStr(op["agent"]), asStr(op["recorder"]), asInt64(op["since"]), asInt64(op["until"]))
			if err != nil {
				t.Fatal(err)
			}
			got = answerJSON(answer)
		case "has_any":
			found, err := store.HasAny(asStr(op["agent"]), asStr(op["recorder"]))
			if err != nil {
				t.Fatal(err)
			}
			got = found
		case "fetched":
			var starts []int64
			for _, s := range asList(op["starts"]) {
				starts = append(starts, asInt64(s))
			}
			done, err := store.Fetched(asStr(op["agent"]), asStr(op["recorder"]), starts)
			if err != nil {
				t.Fatal(err)
			}
			var list []int64
			for s := range done {
				list = append(list, s)
			}
			sort.Slice(list, func(a, b int) bool { return list[a] < list[b] })
			out := []any{}
			for _, s := range list {
				out = append(out, s)
			}
			got = out
		case "maintain":
			var ids []string
			for _, id := range asList(op["agents"]) {
				ids = append(ids, id.(string))
			}
			if err := store.Maintain(ids, i64(asInt64(op["now"]))); err != nil {
				t.Fatal(err)
			}
			got = store.version
		case "dump":
			got = dbDump(t, store.Path)
		}
		wantRaw := []byte(asStr(results[i]))
		if asStr(op["op"]) == "dump" {
			// CPython keeps each sqlite3 connection until the cyclic GC
			// collects it, so its -wal/-shm files exist or not depending on
			// when the GC last ran; Go closes every connection at once (the
			// last close checkpoints and removes them). Only the database
			// file itself is compared.
			wantMap := parseJSONMap(t, wantRaw)
			wantMap["files"] = []any{filepath.Base(store.Path)}
			wantRaw = mustMarshal(t, wantMap)
		}
		wantVal, err := testjson.Parse(wantRaw)
		if err != nil {
			t.Fatal(err)
		}
		if diff := diffRefVal(t, got, wantVal); diff != "" {
			if len(diff) > 600 {
				diff = diff[:600]
			}
			t.Fatalf("op %d %s: %s", i, asStr(op["op"]), diff)
		}
	}
}

func numericQueryChange(c map[string]any) bool {
	resource := asStr(c["resource"])
	for _, pair := range pairsOf(asList(c["args"])) {
		if resource == "logs" && pair[0] == "after" {
			switch pair[1] {
			case "١٢", "²", "①", "０":
				return true
			}
		}
		if strings.HasSuffix(resource, "/delay") && pair[0] == "timeout" {
			switch pair[1] {
			case "٣٠٠٠", "０５０００":
				return true
			}
		}
	}
	return false
}
