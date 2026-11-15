package monitor

// A07 reference corpus from CPython 3.11 (testdata/gen_a07_reference.py →
// testdata/a07_reference.json.gz): mihomo_panel.call, oxidns_cache.call and
// oxidns_rule_update.call on edge inputs and Agent answers (outcome and the
// request that reached the Agent), _newer_release, the version.txt rule,
// last_complete_slot_end, and a ranking history scenario replayed operation
// by operation with every stored row compared.

import (
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func loadA07(t *testing.T) *jsonx.Object {
	t.Helper()
	f, err := os.Open("testdata/a07_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := readAllA07(r)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func readAllA07(r *gzip.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 64*1024)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}

func sortedDump(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonx.Encode(v, jsonx.Options{SortKeys: true, EnsureASCII: true})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// outcomeOf is the corpus' outcome of a call: {"ok": result}, the
// MonitorError fields, or {"exception": ...} for any other error.
func outcomeOf(result any, err error) any {
	if err == nil {
		return jsonx.Obj("ok", result)
	}
	var flushErr *FlushError
	if errors.As(err, &flushErr) {
		return jsonx.Obj("error", flushErr.Err.Code, "status", flushErr.Err.Status, "message", flushErr.Err.Message,
			"flushed", flushErr.Flushed)
	}
	var e *Error
	if errors.As(err, &e) {
		return jsonx.Obj("error", e.Code, "status", e.Status, "message", e.Message)
	}
	return jsonx.Obj("exception", "")
}

func caseClient(c *jsonx.Object) *replyClient {
	reply := c.GetList("reply")
	client := &replyClient{}
	if reply[0] == "raise" {
		client.add(errors.New("refused"))
	} else {
		body, _ := base64.StdEncoding.DecodeString(reply[1].(string))
		status, _ := jsonx.AsInt(reply[0])
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
			data = string(r.Body)
		}
		headers := jsonx.NewObject()
		for _, key := range []string{"Authorization", "Accept", "Content-Type"} {
			if value := r.Header.Get(key); value != "" {
				headers.Set(key, value)
			}
		}
		out = append(out, jsonx.Obj("method", r.Method, "url", lowerOrigin(r.URL), "data", data, "headers", headers,
			"timeout", []any{jsonx.Int(int64(r.Timeout.Connect.Seconds())), jsonx.Int(int64(r.Timeout.Read.Seconds()))},
			"trust_env", !r.NoProxy, "allow_redirects", !r.NoRedirects, "stream", r.MaxBody > 0))
	}
	return out
}

func normalizeSeen(t *testing.T, text string) string {
	value, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range value.([]any) {
		o := item.(*jsonx.Object)
		o.Set("url", lowerOrigin(o.GetString("url")))
	}
	return sortedDump(t, value)
}

func caseManager(t *testing.T, c *jsonx.Object, client *replyClient) (*fakeManager, string) {
	agent := c.GetObject("agent")
	id, _ := agent.Value("id").(string)
	return &fakeManager{agents: map[string]*jsonx.Object{id: agent.DeepCopy()}, dataDir: t.TempDir(), client: client}, id
}

// nonLatin1Token reports cases whose token http.client could not encode:
// the corpus' patched Session.request never reaches http.client, so Python
// answered them; the real requests stack raises UnicodeEncodeError
// (AGENT_UNREACHABLE), as the Flask differential confirms.
func nonLatin1Token(c *jsonx.Object) bool {
	token, ok := c.GetObject("agent").Value("token").(string)
	if !ok {
		return false
	}
	_, ok = latin1(token)
	return !ok
}

func compareCall(t *testing.T, label string, c *jsonx.Object, result any, err error, client *replyClient) {
	t.Helper()
	if nonLatin1Token(c) {
		wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
		return
	}
	got := outcomeOf(result, err)
	want, perr := jsonx.ParseString(c.GetString("outcome"))
	if perr != nil {
		t.Fatal(perr)
	}
	if _, ok := want.(*jsonx.Object).Get("exception"); ok {
		if _, raised := got.(*jsonx.Object).Get("exception"); !raised {
			t.Errorf("%s: Python raised %s, Go %s", label, c.GetString("outcome"), sortedDump(t, got))
		}
	} else if sortedDump(t, got) != sortedDump(t, want) {
		t.Errorf("%s:\n go %s\n py %s", label, sortedDump(t, got), sortedDump(t, want))
	}
	if g, w := sortedDump(t, seenOf(client)), normalizeSeen(t, c.GetString("seen")); g != w {
		t.Errorf("%s seen:\n go %s\n py %s", label, g, w)
	}
}

func pairsOf(list []any) [][2]string {
	out := [][2]string{}
	for _, item := range list {
		pair := item.([]any)
		out = append(out, [2]string{pair[0].(string), pair[1].(string)})
	}
	return out
}

func TestReferencePanelCalls(t *testing.T) {
	ref := loadA07(t)
	for i, item := range ref.GetList("panel") {
		c := item.(*jsonx.Object)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		result, err := PanelCall(ctx, m, id, c.GetString("resource"), c.GetString("method"), pairsOf(c.GetList("args")),
			c.Value("body"), "")
		compareCall(t, sortedDump(t, jsonx.Obj("i", i, "resource", c.Value("resource"), "args", c.Value("args"))), c, result, err, client)
	}
}

func TestReferenceCacheCalls(t *testing.T) {
	ref := loadA07(t)
	for i, item := range ref.GetList("cache") {
		c := item.(*jsonx.Object)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		result, err := CacheCall(ctx, m, id, c.GetString("method"), c.Value("tags"))
		compareCall(t, sortedDump(t, jsonx.Obj("i", i, "tags", c.Value("tags"), "reply", c.Value("reply"))), c, result, err, client)
	}
}

func TestReferenceRuleUpdateCalls(t *testing.T) {
	ref := loadA07(t)
	for i, item := range ref.GetList("rule_update") {
		c := item.(*jsonx.Object)
		client := caseClient(c)
		m, id := caseManager(t, c, client)
		data, status, err := RuleUpdateCall(ctx, m, id, c.GetString("method"), c.Value("paths"))
		var result any
		if err == nil {
			result = []any{data, jsonx.Int(int64(status))}
		}
		compareCall(t, sortedDump(t, jsonx.Obj("i", i, "paths_len", len(c.GetList("paths")), "reply", c.Value("reply"))), c, result, err, client)
	}
}

func TestReferencePanelHelpers(t *testing.T) {
	ref := loadA07(t)
	for _, item := range ref.GetList("newer_release") {
		c := item.([]any)
		if got := newerRelease(c[0].(string), c[1].(string)); got != c[2].(bool) {
			t.Errorf("newer_release(%q, %q) = %v", c[0], c[1], got)
		}
	}
	for _, item := range ref.GetList("core_version_texts") {
		c := item.([]any)
		raw, _ := base64.StdEncoding.DecodeString(c[0].(string))
		version := pyStrip(agents.DecodeUTF8Replace(raw))
		var got any
		if coreVersionPattern.MatchString(version) {
			got = version
		}
		if !jsonx.Equal(got, c[1]) {
			t.Errorf("version.txt %q: %v want %v", raw, got, c[1])
		}
	}
	for _, item := range ref.GetList("last_complete_slot_end") {
		c := item.([]any)
		n, _ := jsonx.AsInt(c[0])
		want, _ := jsonx.AsInt(c[1])
		if got := LastCompleteSlotEnd(n); got != want {
			t.Errorf("last_complete_slot_end(%d) = %d want %d", n, got, want)
		}
	}
}

// --- ranking scenario ---------------------------------------------------------------

func slotFromCorpus(o *jsonx.Object) *Slot {
	slot := &Slot{Records: o.GetInt("records"), Partial: o.Value("partial") == true, Rows: map[string]*Counts{}}
	if blocked, ok := jsonx.AsInt(o.Value("blocked")); ok && o.Value("blocked") != nil {
		slot.Blocked = &blocked
	}
	rows := o.GetObject("rows")
	for kind, value := range rows.All() {
		counts := &Counts{Values: map[string]int64{}}
		for _, pair := range value.([]any) {
			p := pair.([]any)
			n, _ := jsonx.AsInt(p[1])
			counts.Add(p[0].(string), n)
		}
		slot.Rows[kind] = counts
	}
	return slot
}

func answerJSON(a *HistoryAnswer) *jsonx.Object {
	rows := jsonx.NewObject()
	for _, kind := range RankingKinds {
		list := []any{}
		for _, row := range a.Rows[kind] {
			list = append(list, []any{row.Key, jsonx.Int(row.Count)})
		}
		rows.Set(kind, list)
	}
	var first, last any
	if a.Slots != 0 {
		first, last = jsonx.Int(a.First), jsonx.Int(a.Last)
	}
	return jsonx.Obj("records", a.Records, "blocked_records", a.BlockedRecords, "blocked_slots", a.BlockedSlots,
		"slots", a.Slots, "partial_slots", a.PartialSlots, "first", first, "last", last, "rows", rows)
}

func sqlValue(v any) any {
	switch x := v.(type) {
	case int64:
		return jsonx.Int(x)
	case []byte:
		return string(x)
	case nil:
		return nil
	}
	return v
}

// dbDump is the generator's db_dump with Go's driver.
func dbDump(t *testing.T, path string) *jsonx.Object {
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
	out := jsonx.Obj("journal_mode", mode,
		"master", rowsOf("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name"))
	for _, table := range []string{"ranking_periods", "ranking_rows", "ranking_slots"} {
		out.Set(table, rowsOf("SELECT rowid, * FROM "+table+" ORDER BY rowid"))
	}
	db.Close()
	header := make([]byte, 100)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Read(header)
	f.Close()
	out.Set("header_18_20", []any{jsonx.Int(int64(header[18])), jsonx.Int(int64(header[19]))})
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
	out.Set("files", list)
	return out
}

func TestReferenceRankingScenario(t *testing.T) {
	ref := loadA07(t).GetObject("ranking")
	ops, results := ref.GetList("ops"), ref.GetList("results")
	store := NewRankingStore(t.TempDir())
	for i, item := range ops {
		op := item.(*jsonx.Object)
		var got any
		switch op.GetString("op") {
		case "collect":
			var records []rankRecord
			for _, r := range op.GetList("records") {
				f := r.([]any)
				at, _ := jsonx.AsInt(f[0])
				records = append(records, rankRecord{at, f[1].(string), f[2].(string), f[3] == true})
			}
			useAgent(t, newFakeAgent(records).do)
			agentID := op.GetString("agent")
			stored, err := CollectAgent(ctx, nil, store, jsonx.Obj("id", agentID, "name", agentID, "version", HistoryAgentVersion),
				[]string{op.GetString("recorder")}, i64(op.GetInt("now")), int(op.GetInt("budget")), int(op.GetInt("retention")), 0)
			if err != nil {
				t.Fatal(err)
			}
			var caught any
			if end, ok := store.CaughtUp(agentID); ok {
				caught = jsonx.Int(end)
			}
			got = []any{jsonx.Int(int64(stored)), caught}
		case "add":
			added, err := store.AddSlot(op.GetString("agent"), op.GetString("recorder"), op.GetInt("start"), slotFromCorpus(op.GetObject("slot")))
			if err != nil {
				t.Fatal(err)
			}
			got = added
		case "query":
			answer, err := store.Query(op.GetString("agent"), op.GetString("recorder"), op.GetInt("since"), op.GetInt("until"))
			if err != nil {
				t.Fatal(err)
			}
			got = answerJSON(answer)
		case "has_any":
			found, err := store.HasAny(op.GetString("agent"), op.GetString("recorder"))
			if err != nil {
				t.Fatal(err)
			}
			got = found
		case "fetched":
			var starts []int64
			for _, s := range op.GetList("starts") {
				n, _ := jsonx.AsInt(s)
				starts = append(starts, n)
			}
			done, err := store.Fetched(op.GetString("agent"), op.GetString("recorder"), starts)
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
				out = append(out, jsonx.Int(s))
			}
			got = out
		case "maintain":
			var ids []string
			for _, id := range op.GetList("agents") {
				ids = append(ids, id.(string))
			}
			if err := store.Maintain(ids, i64(op.GetInt("now"))); err != nil {
				t.Fatal(err)
			}
			got = jsonx.Int(store.version)
		case "dump":
			got = dbDump(t, store.Path)
		}
		want, err := jsonx.ParseString(results[i].(string))
		if err != nil {
			t.Fatal(err)
		}
		if op.GetString("op") == "dump" {
			// CPython keeps each sqlite3 connection until the cyclic GC
			// collects it, so its -wal/-shm files exist or not depending on
			// when the GC last ran; Go closes every connection at once (the
			// last close checkpoints and removes them). Only the database
			// file itself is compared.
			want.(*jsonx.Object).Set("files", []any{filepath.Base(store.Path)})
		}
		if g, w := sortedDump(t, got), sortedDump(t, want); g != w {
			if len(g) > 600 {
				g, w = g[:600], w[:600]
			}
			t.Fatalf("op %d %s:\n go %s\n py %s", i, op.GetString("op"), g, w)
		}
	}
}
