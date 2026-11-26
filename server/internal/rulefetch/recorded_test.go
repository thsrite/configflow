package rulefetch

import (
	"bufio"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// recordedValue turns the recorder's {"__bytes__": hex} back into []byte.
func recordedValue(v any) any {
	if o, ok := v.(*orderedObj); ok && o.len() == 1 {
		if h, ok := o.val("__bytes__").(string); ok {
			data, _ := hex.DecodeString(h)
			return data
		}
	}
	return v
}

// Node positions were Python implementation details. Boundary replay retains
// the sequence kind/style; payload values and edit results are checked separately.
func nodeFingerprint(n *yamlx.Node) any {
	if n == nil {
		return nil
	}
	return newOrderedObj("__node__", []any{"SequenceNode", n.Style&yaml.FlowStyle != 0})
}

func boundaryNodeFingerprint(v any) any {
	if o, ok := v.(*orderedObj); ok && o.len() == 1 {
		if parts, ok := o.val("__node__").([]any); ok && len(parts) == 4 {
			return newOrderedObj("__node__", []any{parts[0], parts[3]})
		}
	}
	if values, ok := v.([]any); ok {
		out := make([]any, len(values))
		for i, item := range values {
			out[i] = boundaryNodeFingerprint(item)
		}
		return out
	}
	return v
}

func recordedClass(err error) string {
	var remote *RemoteRuleValidationError
	switch {
	case errors.As(err, &remote):
		return "RemoteRuleValidationError"
	case errors.Is(err, repo.ErrValidation):
		return "ProfileValidationError"
	}
	return errorClass(err)
}

func errorClass(err error) string {
	if class := errorClassOf(err); class != "" {
		return class
	}
	return "ValueError"
}

func recordedError(err error) any {
	o := newOrderedObj("class", recordedClass(err), "text", err.Error())
	var remote *RemoteRuleValidationError
	if errors.As(err, &remote) {
		o.set("errors", nonNilProblems(remote.Errors))
	}
	return o
}

func noneString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if !x {
			return ""
		}
		return "True"
	case string:
		return x
	case json.Number:
		if x == "0" {
			return ""
		}
		return string(x)
	}
	return fmt.Sprint(v)
}

func stringSlice(list []any) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contentDigestValue(value any) string {
	switch v := value.(type) {
	case string:
		return "s" + dataDigest([]byte(v))
	case []byte:
		return "b" + dataDigest(v)
	}
	return ""
}

// payloadFor derives the payload node Python passed in from the content (it
// came from read_rule_payload of the same content); the fingerprint check
// catches a call where it did not.
func payloadFor(content string, want any) (*yamlx.Node, error) {
	if want == nil {
		return nil, nil
	}
	_, node, err := ReadRulePayload(content)
	if err != nil {
		return nil, err
	}
	if encode(nodeFingerprint(node)) != encode(boundaryNodeFingerprint(want)) {
		return nil, fmt.Errorf("payload node %s differs from %s", encode(nodeFingerprint(node)), encode(want))
	}
	return node, nil
}

func replayRecorded(row *orderedObj) (any, error) {
	args := row.obj("args")
	arg := func(name string) any { return recordedValue(args.val(name)) }
	str := func(name string) string { s, _ := args.val(name).(string); return s }
	obj := func(name string) *orderedObj { o, _ := args.val(name).(*orderedObj); return o }
	hostURL, _ := row.val("host_url").(string)
	switch row.str("fn") {
	case "content_digest":
		if d := contentDigestValue(arg("content")); d != "" {
			return d, nil
		}
		return nil, nil
	case "classical_line_error":
		if message := rules.ClassicalLineError(str("value")); message != "" {
			return message, nil
		}
		return nil, nil
	case "inspect_domain_content":
		return runInspect(args.val("content"), args.val("behavior"), args.val("rule_format"))
	case "normalize_classical_lines":
		content, warnings, err := NormalizeClassical(str("content"))
		return []any{content, nonNilProblems(warnings)}, err
	case "normalize_mihomo_yaml":
		content, warnings := NormalizeYAML(str("content"))
		return []any{content, nonNilProblems(warnings)}, nil
	case "normalize_mihomo_ruleset":
		content, warnings, err := NormalizeRuleset(str("content"), argText(args.val("behavior")), argFormat(args.val("rule_format")))
		return []any{content, nonNilProblems(warnings)}, err
	case "validate_domain_content":
		return nil, runValidateDomain(obj("rule"), obj("previous"))
	case "validate_remote_rule_content":
		format, _ := args.val("rule_format").(string)
		return nil, runValidateRemote(arg("content"), obj("rule"), obj("config_data"), format)
	case "read_rule_payload":
		lines, node, err := ReadRulePayload(str("content"))
		return []any{strs(lines), nodeFingerprint(node)}, err
	case "append_rule_lines", "append_rule_payload":
		payload, err := payloadFor(str("content"), args.val("payload"))
		if err != nil {
			return nil, err
		}
		if row.str("fn") == "append_rule_payload" {
			return AppendRulePayload(str("content"), str("line"), payload)
		}
		return AppendRuleLines(str("content"), stringSlice(args.list("lines")), payload)
	case "remove_rule_payload":
		answers := row.obj("matches")
		var unknown []string
		content, removed, err := RemoveRulePayload(str("content"), func(line string) bool {
			answer, ok := answers.val(line).(bool)
			if !ok {
				unknown = append(unknown, line)
			}
			return answer
		})
		if len(unknown) > 0 {
			return nil, fmt.Errorf("matches asked for unrecorded lines %q", unknown)
		}
		return []any{content, strs(removed)}, err
	case "site_title":
		return SiteTitle(str("title")), nil
	case "clean_text":
		return CleanText(str("value"), args.intVal("limit")), nil
	case "_meta_content":
		return metaContent(str("value")), nil
	case "known_service":
		if s := LookupService(str("host")); s != nil {
			return s, nil
		}
		return nil, nil
	case "page_identity":
		return runPageIdentity(obj("result"), str("host"), args.val("checked_at")), nil
	case "resolve_remote_rule_source":
		return runResolveSource(str("url"), obj("config"), noneString(args.val("source_id")))
	case "validate_rule_url":
		u, err := ValidateRuleURL(str("url"))
		if err != nil {
			return nil, err
		}
		return ruleURLRow(u), nil
	case "normalize_rule_fetch_proxy":
		return runNormalizeProxy(args.val("value"))
	case "is_internal_rule_url":
		return InternalURL(str("url"), decodeSourceConfig(obj("config_data"), false), hostURL)
	case "get_rule_fetch_proxy":
		return FetchProxy(decodeSourceConfig(obj("config_data"), false))
	case "rule_request_options":
		proxy, err := ProxyFor(str("url"), decodeSourceConfig(obj("config_data"), false), hostURL)
		return newOrderedObj("proxies", newOrderedObj("http", proxy, "https", proxy)), err
	case "is_github_url":
		return IsGithubURL(str("url")), nil
	case "github_download_candidates":
		candidates, err := DownloadCandidates(str("url"), decodeSourceConfig(obj("config_data"), false), hostURL)
		var out []any
		for _, c := range candidates {
			out = append(out, []any{c.Label, c.URL})
		}
		return out, err
	}
	return nil, fmt.Errorf("unknown function %s", row.str("fn"))
}

// TestRecordedSuiteCalls replays testdata/c14_recorded.jsonl.gz: every call of
// C14's public functions made by the whole Python test suite
// (testdata/c14_record.py).
func TestRecordedSuiteCalls(t *testing.T) {
	f, err := os.Open("testdata/c14_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	rows, failures := 0, map[string]int{}
	classes := map[string]int{}
	for sc.Scan() {
		value, err := parseOrderedJSON(sc.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*orderedObj)
		rows++
		fn := row.str("fn")
		if droppedURLRow(fn) {
			continue
		}
		got, gotErr := replayRecorded(row)
		var gotText, wantText string
		if want, isErr := row.get("err"); isErr {
			wantText = encode(classOnly(want))
			if gotErr != nil {
				gotText = encode(classOnly(recordedError(gotErr)))
			} else {
				gotText = "ok " + encode(got)
			}
		} else {
			want, _ := wantURLRow(fn, row.val("ok"))
			wantText = encode(boundaryNodeFingerprint(want))
			if gotErr != nil {
				gotText = "error " + encode(recordedError(gotErr))
			} else {
				gotText = encode(got)
			}
		}
		if gotText != wantText {
			args := row.obj("args")
			if url, _ := args.val("url").(string); urlAccepted(fn, url, args.val("result")) {
				continue
			}
			if category := nativeReplayDifference(t, "recorded", fn, row.val("args"), gotText, wantText); category != "" {
				classes[category]++
				continue
			}
			failures[fn]++
			if failures[fn] <= 3 {
				t.Errorf("%s (%s)\nargs %.600s\n got %.800s\nwant %.800s", fn, row.str("test"),
					encode(row.val("args")), gotText, wantText)
			}
		}
	}
	t.Logf("N7-3b classifications: %v", classes)
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Errorf("differences by function: %v (of %d calls)", failures, rows)
	}
	if rows < 100000 {
		t.Fatalf("only %d recorded calls", rows)
	}
	t.Logf("%d recorded calls replayed", rows)
}
