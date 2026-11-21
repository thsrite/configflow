package model

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The O10-T2 reference corpus pins the codec's external behaviour from before
// the single-validation change: for a deterministic set of synthetic inputs
// and edits it records the outcome (error text, or a digest of the output
// bytes, plus FieldIssues) of DecodeRecord, Marshal, EncodeRecord through
// MarshalJSON, SetMemberJSON, Map/Fields/Config/Document codecs and the
// internal validJSON/parseObject. testdata/o10t2_reference.txt was written by
// the previous implementation (CONFIGFLOW_O10T2_GOLDEN=1); the current one must
// reproduce it line for line.
const o10t2Golden = "testdata/o10t2_reference.txt"

func o10t2Outcome(data []byte, err error) string {
	if err != nil {
		return "err:" + err.Error()
	}
	sum := sha256.Sum256(data)
	return "ok:" + hex.EncodeToString(sum[:12])
}

func o10t2Issues(issues []FieldIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Path+"="+string(issue.Kind))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func o10t2Object(data []byte) string {
	object, err := parseObject(data)
	if err != nil {
		return "err:" + err.Error()
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%q|%q|%q", object.data, object.opening, object.closing)
	for _, m := range object.members {
		fmt.Fprintf(&out, "|%q:%q:%q:%q:%q:%q", m.name, m.key, m.leading, m.separator, m.value, m.trailing)
	}
	sum := sha256.Sum256([]byte(out.String()))
	return "ok:" + hex.EncodeToString(sum[:12])
}

// o10t2Values are member values for edits: valid values, fragments that are
// only valid inside a larger document, duplicates, bad UTF-8 and deep nesting.
var o10t2Values = []string{
	`1`, ` 1 `, `"x"`, `null`, `true`, `{}`, `[]`, `{"a":1}`, `[1,2]`, `-0.5e+3`,
	`1,2`, `1,"a":2`, `1}, "q": {"z":2`, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `[{"k":1,"k":2}]`,
	`{"\ud800":1,"\udc00":2}`, `{"\ud800":1,"�":2}`, "\"\xff\"", "\"\xed\xa0\x80\"", `"a` + "\x01" + `"`,
	``, ` `, `01`, `1.`, `.5`, `+1`, `tru`, `nul`, `"\x"`, `"\u12"`, `{"a" 1}`, `[1,]`, `{,}`, `{"a":1,}`,
	strings.Repeat("[", 10000) + strings.Repeat("]", 10000),
	strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	strings.Repeat(`{"a":`, 3000) + "1" + strings.Repeat("}", 3000),
	`{"deployment_id":"d","health":{"ok":true},"events":[{"stage":"x","future":[1,{"a":null}]}]}`,
	`{"deployment_id":"d","events":"wrong","dns_checks":[{"name":1}]}`,
}

func o10t2Deployment(r *rand.Rand, id int) string {
	var members []string
	members = append(members, fmt.Sprintf(`"deployment_id":"dep-%d"`, id), `"success":true`, `"status":"ok"`)
	if r.IntN(2) == 0 {
		members = append(members, `"health":{"checks":[{"name":"dns","ok":true},{"name":"http","latency":12.5}],"summary":"例 <>&"}`)
	}
	events := []string{}
	for i := range r.IntN(4) {
		events = append(events, fmt.Sprintf(`{"stage":"s%d","message":"m","future":{"n":%d,"list":[1,2,{"z":null}]}}`, i, i))
	}
	members = append(members, `"events":[`+strings.Join(events, ",")+`]`)
	if r.IntN(3) == 0 {
		members = append(members, `"dns_checks":[{"name":"a","status":"ok"},{"name":"b","status":7}]`)
	}
	if r.IntN(3) == 0 {
		members = append(members, `"config_revision":"12"`, `"http_status":2e2`)
	}
	if r.IntN(4) == 0 {
		members = append(members, `"future_member":{"x":[true,false,null],"y":"é"}`)
	}
	r.Shuffle(len(members), func(i, j int) { members[i], members[j] = members[j], members[i] })
	return "{" + strings.Join(members, ",") + "}"
}

func o10t2Agent(r *rand.Rand) string {
	members := []string{`"id":"agent-1"`, `"name":"例 agent"`, `"host":"192.0.2.10"`, `"port":8080`, `"token":"t"`}
	if r.IntN(2) == 0 {
		members = append(members, `"enabled":true`, `"config_revision":3`)
	} else {
		members = append(members, `"enabled":"yes"`, `"config_revision":"x"`)
	}
	deployments := []string{}
	for i := range r.IntN(5) {
		deployments = append(deployments, fmt.Sprintf(`"dep-%d":%s`, i, o10t2Deployment(r, i)))
	}
	members = append(members, `"deployments":{`+strings.Join(deployments, ",")+`}`)
	if r.IntN(2) == 0 {
		members = append(members, `"latest_deployment":`+o10t2Deployment(r, 99))
	}
	if r.IntN(2) == 0 {
		members = append(members, `"ipv6_verifications":{"v1":{"status":"ok","checks":[{"id":"c","ok":true}]},"v2":false}`)
	}
	if r.IntN(2) == 0 {
		members = append(members, `"logging_config":{"enabled":{"level":"debug"},"other":1}`)
	}
	if r.IntN(3) == 0 {
		members = append(members, `"rule_sync":{"settings":{"enabled":true},"state":{"status":"idle"}}`)
	}
	if r.IntN(3) == 0 {
		members = append(members, `"future":{"nested":{"deep":[{"a":1},{"b":[2,3]}]}}`)
	}
	r.Shuffle(len(members), func(i, j int) { members[i], members[j] = members[j], members[i] })
	sep := []string{",", " , ", ",\n  ", "\t,"}[r.IntN(4)]
	return []string{"", " ", "\n"}[r.IntN(3)] + "{" + strings.Join(members, sep) + "}" + []string{"", " ", "\t\n"}[r.IntN(3)]
}

// o10t2Corrupt applies one deterministic corruption to valid JSON.
func o10t2Corrupt(r *rand.Rand, data string) string {
	if len(data) < 4 {
		return data
	}
	at := r.IntN(len(data))
	switch r.IntN(8) {
	case 0: // duplicate the first member name of a random object
		if i := strings.Index(data[at:], `{"`); i >= 0 {
			i += at
			if end := strings.Index(data[i+2:], `"`); end >= 0 {
				name := data[i+2 : i+2+end]
				return data[:i+1] + `"` + name + `":0,` + data[i+1:]
			}
		}
		return data
	case 1:
		return data[:at] + "\xff" + data[at:]
	case 2:
		return data[:at]
	case 3:
		return data[:at] + "," + data[at:]
	case 4:
		return data[:at] + string(data[at]^0x20) + data[at+1:]
	case 5: // escaped spelling of an existing name next to it
		if i := strings.Index(data, `"name":`); i >= 0 {
			return data[:i] + `"name":"y",` + data[i:]
		}
		return data
	case 6:
		return data[:at] + " " + data[at:]
	default:
		return data[:at] + `"` + data[at:]
	}
}

func o10t2Cases() []string {
	var lines []string
	add := func(name, outcome string) { lines = append(lines, name+"\t"+outcome) }
	r := rand.New(rand.NewPCG(10, 2))

	for _, value := range o10t2Values {
		key := fmt.Sprintf("v%x", sha256.Sum256([]byte(value)))[:13]
		add("valid/"+key, o10t2Outcome(nil, validJSON([]byte(value))))
		add("object/"+key, o10t2Object([]byte(value)))
		add("marshal-raw/"+key, o10t2Outcome(Marshal(json.RawMessage(value))))
		add("marshal-raw-list/"+key, o10t2Outcome(Marshal([]json.RawMessage{json.RawMessage(value), json.RawMessage("3")})))
		add("marshal-go-map/"+key, o10t2Outcome(Marshal(map[string]any{"k": map[string]json.RawMessage{"a": json.RawMessage(value)}, "z": 1})))
		add("marshal-any/"+key, o10t2Outcome(Marshal([]any{json.RawMessage(value), map[string]any{"b": json.RawMessage(value)}})))
		var fields Fields
		setErr := fields.Set("a", json.RawMessage(value))
		add("fields-set/"+key, o10t2Outcome(nil, setErr))
		add("fields-marshal/"+key, o10t2Outcome(Marshal(fields)))
		var rawMap Map[json.RawMessage]
		rawMap.Set("a", json.RawMessage(value))
		rawMap.Set("b", json.RawMessage(`2`))
		add("map-raw/"+key, o10t2Outcome(Marshal(rawMap)))
		add("map-raw-direct/"+key, o10t2Outcome(rawMap.MarshalJSON()))
		var dynamic Dynamic
		add("dynamic/"+key, o10t2Outcome(nil, dynamic.UnmarshalJSON([]byte(value))))
		var record sampleRecord
		add("decode-sample/"+key, o10t2Outcome(nil, DecodeRecord([]byte(value), &record, &record.raw)))
		var logging AgentLoggingConfig
		logging.Enabled = json.RawMessage(value)
		add("logging-marshal/"+key, o10t2Outcome(Marshal(logging)))
		add("logging-direct/"+key, o10t2Outcome(logging.MarshalJSON()))
		var deployment Deployment
		err := SetMemberJSON(&deployment, &deployment.raw, "health", json.RawMessage(value))
		add("deployment-set-health/"+key, o10t2Outcome(nil, err)+o10t2Issues(deployment.raw.Issues()))
		add("deployment-health/"+key, o10t2Outcome(Marshal(deployment)))
		deployment.Health = json.RawMessage(value)
		add("deployment-health-assign/"+key, o10t2Outcome(deployment.MarshalJSON()))
		var agent Agent
		agent.Deployments.Set("d", deployment)
		agent.LatestDeployment = &deployment
		add("agent-nested-health/"+key, o10t2Outcome(Marshal(agent)))
		add("agent-nested-health-direct/"+key, o10t2Outcome(agent.MarshalJSON()))
		add("agent-list/"+key, o10t2Outcome(Marshal([]Agent{agent, {ID: "b"}})))
		var decodedAgent Agent
		err = DecodeRecord([]byte(`{"id":"a","deployments":{"d":`+value+`},"latest_deployment":`+value+`}`), &decodedAgent, &decodedAgent.raw)
		add("agent-decode-nested/"+key, o10t2Outcome(nil, err)+o10t2Issues(valueIssues(reflect.ValueOf(&decodedAgent), "")))
		if err == nil {
			add("agent-decode-nested-encode/"+key, o10t2Outcome(Marshal(decodedAgent)))
		}
		lines := TextLines{Lines: []string{value, "\xff"}}
		add("textlines/"+key, o10t2Outcome(Marshal(lines)))
	}

	// Depth limit at every former check point: a value that is valid alone
	// exceeds the limit once records, lists and maps wrap it.
	for depth := 9995; depth <= 10000; depth++ {
		deep := json.RawMessage(strings.Repeat("[", depth) + strings.Repeat("]", depth))
		for _, dup := range []bool{false, true} {
			name := fmt.Sprintf("depth/%d/%v", depth, dup)
			logging := AgentLoggingConfig{Enabled: deep}
			agent := Agent{ID: "a", LoggingConfig: &logging}
			if dup {
				agent.LatestDeployment = &Deployment{Health: json.RawMessage(`{"a":1,"a":2}`)}
			}
			add(name+"/logging-direct", o10t2Outcome(logging.MarshalJSON()))
			add(name+"/agent-direct", o10t2Outcome(agent.MarshalJSON()))
			add(name+"/agent", o10t2Outcome(Marshal(agent)))
			add(name+"/list", o10t2Outcome(Marshal([]Agent{agent})))
			add(name+"/list-list", o10t2Outcome(Marshal([][]Agent{{agent}})))
			add(name+"/go-map", o10t2Outcome(Marshal(map[string]any{"a": []Agent{agent}, "b": json.RawMessage(`1,2`)})))
			var agents Map[Agent]
			agents.Set("a", agent)
			add(name+"/map-direct", o10t2Outcome(agents.MarshalJSON()))
			var fields Fields
			add(name+"/fields-set", o10t2Outcome(nil, fields.Set("x", deep)))
			_ = fields.Set("y", json.RawMessage(`[`+string(deep)+`]`))
			add(name+"/fields-direct", o10t2Outcome(fields.MarshalJSON()))
			profile := Profile{}
			config := Config{Profile: &profile, Agents: []Agent{agent}, HasAgents: dup}
			add(name+"/config-direct", o10t2Outcome(config.MarshalJSON()))
			lines := TextLines{Lines: []string{string(deep)}}
			add(name+"/textlines", o10t2Outcome(Marshal(lines)))
			reversed := Agent{ID: "r", LoggingConfig: &AgentLoggingConfig{Enabled: json.RawMessage(`{"a":1,"a":2}`)}, LatestDeployment: &Deployment{Health: deep}}
			add(name+"/reversed-direct", o10t2Outcome(reversed.MarshalJSON()))
			add(name+"/reversed-list", o10t2Outcome(Marshal([]Agent{reversed})))
		}
	}

	// Cycles and non-model values through Marshal.
	cyclic := []any{1}
	cyclic[0] = cyclic
	add("marshal-cycle", o10t2Outcome(Marshal(cyclic)))
	selfMap := map[string]any{}
	selfMap["x"] = selfMap
	add("marshal-cycle-map", o10t2Outcome(Marshal(selfMap)))
	shared := []any{"x"}
	add("marshal-shared", o10t2Outcome(Marshal([]any{shared, shared})))
	add("marshal-number-bad", o10t2Outcome(Marshal(json.Number("1e"))))
	add("marshal-number", o10t2Outcome(Marshal(json.Number("12345678901234567890"))))
	add("marshal-string-bad-utf8", o10t2Outcome(Marshal("a\xffb<>&")))
	add("marshal-int-key-map", o10t2Outcome(Marshal(map[int]int{1: 2})))
	add("marshal-struct", o10t2Outcome(Marshal(struct{ A int }{1})))
	add("marshal-nil", o10t2Outcome(Marshal(nil)))
	add("marshal-nil-slice", o10t2Outcome(Marshal([]Agent(nil))))
	add("marshal-bytes", o10t2Outcome(Marshal([]byte("ab"))))
	add("config-noprofile", o10t2Outcome(Marshal(Config{})))

	// Synthetic Agent documents, unchanged, corrupted and edited.
	for i := range 400 {
		source := o10t2Agent(r)
		inputs := []string{source, o10t2Corrupt(r, source), o10t2Corrupt(r, o10t2Corrupt(r, source))}
		for j, input := range inputs {
			name := fmt.Sprintf("agent/%d/%d", i, j)
			add(name+"/valid", o10t2Outcome(nil, validJSON([]byte(input))))
			add(name+"/object", o10t2Object([]byte(input)))
			var agent Agent
			err := DecodeRecord([]byte(input), &agent, &agent.raw)
			add(name+"/decode", o10t2Outcome(nil, err)+o10t2Issues(valueIssues(reflect.ValueOf(&agent), "")))
			if err != nil {
				continue
			}
			add(name+"/encode", o10t2Outcome(Marshal(agent)))
			add(name+"/encode-direct", o10t2Outcome(agent.MarshalJSON()))
			value := o10t2Values[r.IntN(len(o10t2Values))]
			member := []string{"name", "port", "deployments", "latest_deployment", "logging_config", "future", "ipv6_verifications", "enabled"}[r.IntN(8)]
			edited := agent
			err = SetMemberJSON(&edited, &edited.raw, member, json.RawMessage(value))
			add(name+"/set-"+member, o10t2Outcome(nil, err)+o10t2Issues(valueIssues(reflect.ValueOf(&edited), "")))
			if err == nil {
				add(name+"/set-encode", o10t2Outcome(Marshal(edited)))
			}
			edited = agent
			edited.Name = "edited <>& 例"
			if dep, ok := edited.Deployments.Get("dep-0"); ok {
				dep.Health = json.RawMessage(value)
				edited.Deployments.Set("dep-0", dep)
			}
			if edited.LoggingConfig != nil {
				logging := *edited.LoggingConfig
				logging.Enabled = json.RawMessage(o10t2Values[r.IntN(len(o10t2Values))])
				edited.LoggingConfig = &logging
			}
			add(name+"/edit-encode", o10t2Outcome(Marshal(edited)))
			add(name+"/edit-list", o10t2Outcome(Marshal([]Agent{edited, agent})))
			if r.IntN(2) == 0 {
				_ = edited.Clear("deployments")
				add(name+"/clear-encode", o10t2Outcome(edited.MarshalJSON()))
			}
			var deployments Map[Deployment]
			if data, ok, _ := agent.raw.MemberJSON("deployments", agent.Deployments); ok {
				add(name+"/map-decode", o10t2Outcome(nil, json.Unmarshal(data, &deployments)))
				add(name+"/map-encode", o10t2Outcome(Marshal(deployments)))
			}
		}
	}

	// Documents with profiles, wrapped in Config.
	for i := range 60 {
		agent := o10t2Agent(r)
		doc := fmt.Sprintf(`{"schema_version":5,"profiles":{"p":{"id":"p","name":"例","future":{"z":1.0}}},"system":{"_revision":%d,"agents":[%s]},"shared":{}}`, i, agent)
		inputs := []string{doc, o10t2Corrupt(r, doc)}
		for j, input := range inputs {
			name := fmt.Sprintf("document/%d/%d", i, j)
			document, issues, err := DecodeDocument([]byte(input))
			add(name+"/decode", o10t2Outcome(nil, err)+o10t2Issues(issues))
			if err != nil {
				continue
			}
			add(name+"/encode", o10t2Outcome(Marshal(document)))
			add(name+"/encode-direct", o10t2Outcome(document.MarshalJSON()))
			if len(document.Profiles) > 0 {
				config := Config{Profile: document.Profiles[0], ProfileID: "p", Agents: document.System.Agents, HasAgents: i%2 == 0}
				add(name+"/config", o10t2Outcome(Marshal(config)))
				add(name+"/config-direct", o10t2Outcome(config.MarshalJSON()))
			}
		}
	}
	return lines
}

func TestO10T2ReferenceCorpus(t *testing.T) {
	got := o10t2Cases()
	if os.Getenv("CONFIGFLOW_O10T2_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(o10t2Golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(o10t2Golden, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d cases", len(got))
		return
	}
	file, err := os.Open(o10t2Golden)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var want []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		want = append(want, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("case count %d, golden %d", len(got), len(want))
	}
	failures := 0
	for i := range got {
		if got[i] != want[i] {
			failures++
			if failures <= 20 {
				t.Errorf("case %d differs:\n got  %s\n want %s", i, got[i], want[i])
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d of %d cases differ", failures, len(got))
	}
	// Negative control: the corpus must contain both successes and each
	// error class, or the comparison could pass vacuously.
	var ok, invalid, duplicate, cyclic, typ bool
	for _, line := range got {
		ok = ok || strings.Contains(line, "\tok:")
		invalid = invalid || strings.HasSuffix(line, "\terr:"+ErrJSON.Error())
		duplicate = duplicate || strings.Contains(line, "duplicate member")
		cyclic = cyclic || strings.Contains(line, "cyclic value")
		typ = typ || strings.Contains(line, ErrType.Error())
	}
	if !ok || !invalid || !duplicate || !cyclic || !typ {
		t.Fatalf("corpus lacks an outcome class: ok=%v invalid=%v duplicate=%v cyclic=%v type=%v", ok, invalid, duplicate, cyclic, typ)
	}
	if !bytes.Contains([]byte(strings.Join(got, "\n")), []byte("deployments[")) {
		t.Fatal("corpus lacks nested FieldIssues")
	}
}
