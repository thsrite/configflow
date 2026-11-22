package routersplit

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/testgolden"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
	"gopkg.in/yaml.v3"
)

// Replay of every router_split call the Python suite made
// (testdata/r02_record.py → testdata/r02_recorded.jsonl.gz). Collaborator
// answers (profile_sessions, get_profile_agent_bindings, get_profile) are
// served from the record in call order; Go must ask for the same ones.

type recField struct {
	Key   string
	Value any
}

// recObj is an ordered JSON object decoded with encoding/json UseNumber for
// exact replay of recorded Python inputs and pyref golden keys.
type recObj struct {
	fields []recField
	index  map[string]int
}

func newRecObj(pairs ...any) *recObj {
	o := &recObj{index: map[string]int{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func (o *recObj) set(key string, value any) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if i, ok := o.index[key]; ok {
		o.fields[i].Value = value
		return
	}
	o.index[key] = len(o.fields)
	o.fields = append(o.fields, recField{Key: key, Value: value})
}

func (o *recObj) get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	i, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.fields[i].Value, true
}

func (o *recObj) value(key string) any {
	v, _ := o.get(key)
	return v
}

func (o *recObj) str(key string) string {
	s, _ := o.value(key).(string)
	return s
}

func (o *recObj) obj(key string) *recObj {
	v, _ := o.value(key).(*recObj)
	return v
}

func (o *recObj) list(key string) []any {
	v, _ := o.value(key).([]any)
	return v
}

func (o *recObj) intVal(key string) int {
	n, ok := o.value(key).(json.Number)
	if !ok {
		return 0
	}
	i, _ := n.Int64()
	return int(i)
}

func (o *recObj) clone() *recObj {
	if o == nil {
		return nil
	}
	out := newRecObj()
	for _, f := range o.fields {
		out.set(f.Key, cloneRec(f.Value))
	}
	return out
}

func cloneRec(v any) any {
	switch x := v.(type) {
	case *recObj:
		return x.clone()
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneRec(item)
		}
		return out
	default:
		return v
	}
}

func (o *recObj) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := jsonresp.EncodeUTF8(f.Key)
		if err != nil {
			return nil, err
		}
		v, err := encodeCompactBytes(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func encodeCompactBytes(v any) ([]byte, error) {
	switch x := v.(type) {
	case *recObj:
		return x.MarshalJSON()
	case []any:
		if x == nil {
			return []byte("null"), nil
		}
		var b bytes.Buffer
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			raw, err := encodeCompactBytes(item)
			if err != nil {
				return nil, err
			}
			b.Write(raw)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	default:
		return jsonresp.EncodeUTF8(v)
	}
}

func parseRec(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readRecValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing token")
	}
	return v, nil
}

func readRecValue(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		o := newRecObj()
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return nil, err
			}
			v, err := readRecValue(d)
			if err != nil {
				return nil, err
			}
			o.set(k.(string), v)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return o, nil
	case '[':
		out := []any{}
		for d.More() {
			v, err := readRecValue(d)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", delim)
	}
}

type recordedRow struct {
	Test     string
	Fn       string
	Args     *recObj
	Out      any
	Err      *recObj
	Env      []any
	Raw      *recObj
	InputSHA string
	Line     int
}

func loadRecorded(t testing.TB, path string) []recordedRow {
	t.Helper()
	f, err := testgolden.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	var rows []recordedRow
	index := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		value, err := parseRec(line)
		if err != nil {
			t.Fatal(err)
		}
		entry := decodeRecorded(value).(*recObj)
		row := recordedRow{Test: entry.str("test"), Fn: entry.str("fn"), Args: entry.obj("args"),
			Out: entry.value("out"), Err: entry.obj("err"), Env: entry.list("env"), Raw: entry}
		row.InputSHA = n89Hash(line)
		row.Line = index
		index++
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

type repoMarker struct{}

// decodeRecorded turns the recorder's markers back into values: tuples into
// lists, the repository argument into repoMarker.
func decodeRecorded(v any) any {
	switch value := v.(type) {
	case *recObj:
		if len(value.fields) == 1 {
			if items, ok := value.get("__tuple__"); ok {
				return decodeRecorded(items)
			}
			if _, ok := value.get("__repo__"); ok {
				return repoMarker{}
			}
		}
		out := newRecObj()
		for _, f := range value.fields {
			out.set(f.Key, decodeRecorded(f.Value))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = decodeRecorded(item)
		}
		return out
	}
	return v
}

type replayError struct{ class, message string }

func (e *replayError) Error() string      { return e.message }
func (e *replayError) ErrorClass() string { return e.class }

func encode(v any) string {
	b, err := encodeCompactBytes(v)
	if err != nil {
		return fmt.Sprintf("<%v: %#v>", err, v)
	}
	return string(b)
}

// compareError checks a Go error against the recorded exception.
func compareError(want *recObj, got error) string {
	if got == nil {
		return "Python raised " + encode(want) + ", Go returned no error"
	}
	class := "?"
	var py interface{ PythonClass() string }
	var native interface{ ErrorClass() string }
	if errors.As(got, &py) {
		class = py.PythonClass()
	} else if errors.As(got, &native) {
		class = native.ErrorClass()
	}
	var problems []string
	if class != want.str("class") {
		problems = append(problems, fmt.Sprintf("class python %s go %s", want.str("class"), class))
	}
	if got.Error() != want.str("msg") {
		problems = append(problems, fmt.Sprintf("message python %q go %q", want.str("msg"), got.Error()))
	}
	var gateway *gwbase.LinuxGatewayError
	if errors.As(got, &gateway) != (want.value("gateway") == true) {
		problems = append(problems, "isinstance LinuxGatewayError differs")
	}
	if code, ok := want.get("code"); ok {
		var split *RouterSplitError
		if !errors.As(got, &split) || split.Code != code || !testjson.Equal(split.Status, want.value("status")) {
			problems = append(problems, "RouterSplitError code/status differ")
		}
	}
	return strings.Join(problems, "; ")
}

// replayEnv serves the recorded collaborator answers in order.
type replayEnv struct {
	calls    []any
	next     int
	problems []string
}

func (r *replayEnv) take(fn, profileID string) (any, error) {
	if r.next >= len(r.calls) {
		r.problems = append(r.problems, fmt.Sprintf("Go called %s(%q), Python made no further collaborator call", fn, profileID))
		return nil, &replayError{"ReplayError", "unexpected call"}
	}
	entry := r.calls[r.next].(*recObj)
	r.next++
	if entry.str("fn") != fn || !testjson.Equal(entry.list("args"), []any{profileID}) {
		r.problems = append(r.problems, fmt.Sprintf("collaborator call %d: python %s%s go %s(%q)", r.next,
			entry.str("fn"), encode(entry.list("args")), fn, profileID))
	}
	if err := entry.obj("err"); err != nil {
		return nil, &replayError{err.str("class"), err.str("msg")}
	}
	return entry.value("out"), nil
}

type notExpressible string

func (e notExpressible) Error() string { return string(e) }

func profileArg(v any) (string, error) {
	switch value := v.(type) {
	case string:
		return value, nil
	case nil:
		return "", nil
	}
	return "", notExpressible("profile_id is not a string")
}

func recToProfile(v any) *model.Profile {
	b, err := encodeCompactBytes(v)
	p := &model.Profile{}
	if err == nil {
		_ = json.Unmarshal(b, p)
	}
	return p
}

func recToContext(v any) *Context {
	b, err := encodeCompactBytes(v)
	c := &Context{}
	if err == nil {
		_ = json.Unmarshal(b, c)
	}
	return c
}

func recToNode(v any) (*yamlx.Node, error) {
	b, err := encodeCompactBytes(v)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(b)
}

func recFromStruct(v any) any {
	b, _ := jsonresp.EncodeUTF8(v)
	out, _ := parseRec(b)
	return out
}

func recStrings(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func (r *replayEnv) readSessions(profileID string) (*Context, error) {
	out, err := r.take("profile_sessions", profileID)
	if err != nil {
		return nil, err
	}
	raw, err := encodeCompactBytes(out)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	if err = json.Unmarshal(raw, &sessions); err != nil {
		return nil, err
	}
	return SessionContext(sessions)
}

func (r *replayEnv) legacyContext(profileID string) (*recObj, error) {
	bindings, err := r.take("get_profile_agent_bindings", profileID)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeCompactBytes(bindings)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return nil, identityError(messageBindingsInvalid)
	}
	var agents []map[string]json.RawMessage
	if raw, ok := fields["agents"]; !ok || json.Unmarshal(raw, &agents) != nil || agents == nil {
		return nil, identityError(messageBindingsInvalid)
	}
	for _, a := range agents {
		if a == nil {
			return nil, identityError(messageBindingsInvalid)
		}
		var service string
		_ = json.Unmarshal(a["service_type"], &service)
		if service == "mihomo" {
			var identity *string
			if json.Unmarshal(a["id"], &identity) != nil || identity == nil || *identity == "" {
				return nil, identityError(messageBindingsInvalid)
			}
		}
	}
	out, err := r.readSessions(profileID)
	if err != nil {
		return nil, err
	}
	var view struct {
		Agents []model.Agent `json:"agents"`
		Raw    model.Raw
	}
	if err = model.DecodeRecord(encoded, &view, &view.Raw); err != nil {
		return nil, err
	}
	for _, a := range view.Agents {
		if a.ServiceType == "mihomo" && a.ID != "" {
			out.MihomoAgentIDs = append(out.MihomoAgentIDs, a.ID)
		}
	}
	res := recFromStruct(struct {
		SessionGoal       *string       `json:"session_goal"`
		SessionID         *model.String `json:"session_id"`
		SessionCompleted  bool          `json:"session_completed"`
		HasGatewaySession bool          `json:"has_gateway_session"`
		MihomoAgentIDs    []string      `json:"mihomo_agent_ids"`
	}{out.SessionGoal, out.SessionID, out.SessionCompleted, out.HasGatewaySession, out.MihomoAgentIDs})
	o, _ := res.(*recObj)
	return o, nil
}

// runRow calls the typed Go function corresponding to a recorded Python row.
func runRow(row recordedRow, env *replayEnv) (any, error) {
	a := row.Args
	switch row.Fn {
	case "read_marker":
		p := recToProfile(a.value("config"))
		state, marker := ReadMarkerProfile(p)
		if marker == nil {
			return []any{state, nil}, nil
		}
		raw := p.Mihomo.RouterSplit.JSON()
		parsed, _ := parseRec(raw)
		out := parsed.(*recObj)
		out.set("route_rule_set_ids", recStrings(marker.RouteRuleSetIDs))
		return []any{state, out}, nil
	case "is_full_tun":
		state, _ := ReadMarkerProfile(recToProfile(a.value("config")))
		return state == StateValid, nil
	case "route_rule_set_ids":
		return recStrings(ProfileRuleIDs(recToProfile(a.value("config")))), nil
	case "tun_filters":
		n, err := recToNode(a.value("tun"))
		if err != nil {
			return []any{}, nil
		}
		return recStrings(TunFilterPaths(n)), nil
	case "full_tun_document":
		n, err := recToNode(a.value("document"))
		if err != nil || n == nil || n.Kind != yaml.MappingNode {
			return nil, gatewayError(messageDocumentObject)
		}
		out, err := FullTunNode(n)
		if err != nil {
			return nil, err
		}
		raw, err := yamlx.ToJSON(out)
		if err != nil {
			return nil, err
		}
		return parseRec(raw)
	case "full_tun_base_yaml":
		text, ok := a.value("text").(string)
		if !ok {
			return nil, gatewayError(messageBaseText)
		}
		return FullTunYAML(text)
	case "is_full_tun_document":
		n, err := recToNode(a.value("doc"))
		return err == nil && FullTunShape(n), nil
	case "check_generation":
		return nil, CheckProfile(recToProfile(a.value("config")))
	case "require_full_tun_generated":
		n, _ := recToNode(a.value("main"))
		return nil, RequireDocument(n)
	case "legacy_context":
		profileID, err := profileArg(a.value("profile_id"))
		if err != nil {
			return nil, err
		}
		return env.legacyContext(profileID)
	case "needs_context":
		p := recToProfile(a.value("config"))
		s, _ := ReadMarkerProfile(p)
		return s == StateAbsent && GatewayEnabled(p), nil
	case "base_is_full_tun":
		return BaseFullTun(recToProfile(a.value("config"))), nil
	case "legacy_source":
		s, err := LegacyProfileSource(recToProfile(a.value("config")), recToContext(a.value("context")))
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, nil
		}
		return s, nil
	case "publish_legacy_source":
		profileID, err := profileArg(a.value("profile_id"))
		if err != nil {
			return nil, err
		}
		p := recToProfile(a.value("config"))
		s, _ := ReadMarkerProfile(p)
		if s != StateAbsent || profileID == "" {
			return nil, nil
		}
		c, err := env.readSessions(profileID)
		if err != nil {
			return nil, err
		}
		agentID, ok := a.value("agent_id").(string)
		if !ok || agentID == "" {
			return nil, identityError(messageTopologyIdentity)
		}
		c.MihomoAgentIDs = []string{agentID}
		source, err := LegacyProfileSource(p, c)
		if err != nil {
			return nil, err
		}
		if source == "" {
			return nil, nil
		}
		return source, nil
	case "legacy_gateway_agent_ids":
		ids, err := LegacyAgentIDs(recToProfile(a.value("config")), recToContext(a.value("context")))
		return recStrings(ids), err
	case "gateway_was_used":
		source, _ := a.value("source").(string)
		var agent model.Agent
		raw, _ := encodeCompactBytes(a.value("agent"))
		// Since O18 T1b a stored marker that is a number reads as its text
		// (and counts as used); the corpus's non-text markers are compared as
		// the absent marker its independent answers assume.
		if _, text := a.obj("agent").value("gateway_installation_id").(string); !text {
			var members map[string]json.RawMessage
			if json.Unmarshal(raw, &members) == nil {
				delete(members, "gateway_installation_id")
				raw, _ = json.Marshal(members)
			}
		}
		_ = jsondecode.Unmarshal(raw, &agent)
		return WasGatewayUsed(recToProfile(a.value("config")), &agent, source)
	case "mode_from_context":
		s, err := ProfileState(recToProfile(a.value("config")), recToContext(a.value("context")))
		if err != nil {
			return nil, err
		}
		if s.Mode == nil {
			return nil, nil
		}
		return *s.Mode, nil
	case "router_split_state":
		profileID, err := profileArg(a.value("profile_id"))
		if err != nil {
			return nil, err
		}
		cfgVal := a.value("config")
		if cfgVal == nil {
			v, err := env.take("get_profile", profileID)
			if err != nil {
				return nil, err
			}
			cfgVal = v
		}
		ctxVal := a.value("context")
		if ctxVal == nil {
			v, err := env.legacyContext(profileID)
			if err != nil {
				return nil, err
			}
			ctxVal = v
		}
		state, err := ProfileState(recToProfile(cfgVal), recToContext(ctxVal))
		if err != nil {
			return nil, err
		}
		return recFromStruct(state), nil
	}
	return nil, notExpressible("unknown function " + row.Fn)
}

func TestRecordedPythonTests(t *testing.T) { replay(t, "testdata/r02_recorded.jsonl.gz") }

// TestCorpus replays the random corpus generated with real Python
// (testdata/gen_r02.py): malformed markers, gateway settings, base YAML
// (anchors, non-string keys, timestamps, invalid YAML, non-strings), router
// topologies, contexts, sessions and agent bindings.
func TestCorpus(t *testing.T) { replay(t, "testdata/r02_corpus.jsonl.gz") }

func replay(t *testing.T, path string) {
	rows := loadRecorded(t, path)
	independent := n89Expectations(t, path)
	identical, notExpr := map[string]int{}, map[string]int{}
	differ := 0
	nativeClasses := map[string]int{}
	oracle := newNativeOracle(t)
	tests, files := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if row.Fn == "classical_ip_lines" {
			continue // replayed in internal/ippresence
		}
		tests[row.Test] = true
		files[strings.SplitN(row.Test, "::", 2)[0]] = true
		env := &replayEnv{calls: row.Env}
		out, err := runRow(row, env)
		var ne notExpressible
		if errors.As(err, &ne) {
			notExpr[row.Fn]++
			continue
		}
		if expectation, ok := independent[row.Line]; ok {
			if row.InputSHA != expectation.InputSHA256 {
				t.Fatal("independent row input changed")
			}
			n89Check(t, expectation, row, out, err, env.next)
			if len(env.problems) > 0 {
				t.Error(env.problems)
			}
			delete(independent, row.Line)
			identical[row.Fn]++
			continue
		}
		var problems []string
		if row.Err != nil {
			if p := compareError(row.Err, err); p != "" {
				problems = append(problems, p)
			}
		} else if err != nil {
			problems = append(problems, "Go error "+err.Error()+", Python returned "+encode(row.Out))
		} else if encode(out) != encode(row.Out) && !(row.Fn == "full_tun_base_yaml" && sameYAML(out, row.Out)) {
			problems = append(problems, "result\n  python "+encode(row.Out)+"\n  go     "+encode(out))
		}
		if env.next != len(env.calls) {
			problems = append(problems, fmt.Sprintf("Go made %d of Python's %d collaborator calls", env.next, len(env.calls)))
		}
		problems = append(problems, env.problems...)
		if len(problems) > 0 && len(env.problems) == 0 && env.next == len(env.calls) {
			class, matches := oracle.compare(t, row, out, err)
			if !matches && (class == yamlxtest.Typing || class == yamlxtest.Composite || class == yamlxtest.Deep) && row.Fn == "full_tun_base_yaml" && err == nil {
				if source, ok := rowYAML(row); ok {
					if text, ok := out.(string); ok {
						matches = nativeFullTunInvariant(source, text)
					}
				}
			}
			if matches {
				nativeClasses[class]++
				problems = nil
			}
		}
		if len(problems) > 0 {
			differ++
			if differ <= 20 {
				t.Errorf("%s %s(%s):\n  %s", row.Test, row.Fn, encode(row.Args), strings.Join(problems, "\n  "))
			}
			continue
		}
		identical[row.Fn]++
	}
	if len(independent) > 0 {
		t.Fatal("stale independent expectations", len(independent))
	}
	total, notTotal := 0, 0
	for _, n := range notExpr {
		notTotal += n
	}
	var names []string
	for name, n := range identical {
		total += n
		names = append(names, fmt.Sprintf("%s %d", name, n))
	}
	sort.Strings(names)
	t.Logf("N7-3 reader classes: %v", nativeClasses)
	t.Logf("%s router_split: %d identical, %d differ, %d not expressible %v; from %d tests in %d files; identical per function: %s",
		path, total, differ, notTotal, notExpr, len(tests), len(files), strings.Join(names, ", "))
}

// TestEdgeCorpus replays hand-picked edge inputs recorded with real Python
// (testdata/gen_r02_edge.py, review R02): marker types and Unicode ids,
// filter values, base YAML (aliases, invalid dates, special numbers,
// complex keys, deep nesting, tags), router/context shapes, sessions.
func TestEdgeCorpus(t *testing.T) { replay(t, "testdata/r02_edge.jsonl.gz") }

// sameYAML compares two base YAML texts structurally (functional parity:
// anchors/aliases and other formatting are no longer compared): both parse to
// the same document, written out without aliases.
func sameYAML(got, want any) bool {
	g, ok1 := got.(string)
	w, ok2 := want.(string)
	if !ok1 || !ok2 {
		return false
	}
	left, err := yamlx.Parse([]byte(g))
	if err != nil {
		return false
	}
	right, err := yamlx.Parse([]byte(w))
	return err == nil && (yamlx.Equal(left, right) || yamlxtest.SemanticEqual(g, w))
}
