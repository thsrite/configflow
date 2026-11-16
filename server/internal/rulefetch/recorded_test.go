package rulefetch

import (
	"bufio"
	"compress/gzip"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/pyyaml"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// recordedValue turns the recorder's {"__bytes__": hex} back into []byte.
func recordedValue(v any) any {
	if o, ok := v.(*jsonx.Object); ok && o.Len() == 1 {
		if h, ok := o.Value("__bytes__").(string); ok {
			data, _ := hex.DecodeString(h)
			return data
		}
	}
	return v
}

// nodeFingerprint is the recorder's {"__node__": [class, start, end, flow]}.
func nodeFingerprint(n *pyyaml.Node) any {
	if n == nil {
		return nil
	}
	class := map[pyyaml.NodeKind]string{pyyaml.ScalarNode: "ScalarNode", pyyaml.SequenceNode: "SequenceNode",
		pyyaml.MappingNode: "MappingNode"}[n.Kind]
	return jsonx.Obj("__node__", []any{class, jsonx.Int(int64(n.Start.Index)), jsonx.Int(int64(n.End.Index)), n.FlowStyle})
}

func recordedClass(err error) string {
	var remote *RemoteRuleValidationError
	var recursion *pyyaml.RecursionError
	switch {
	case errors.As(err, &remote):
		return "RemoteRuleValidationError"
	case errors.Is(err, repo.ErrValidation):
		return "ProfileValidationError"
	case errors.As(err, &recursion):
		return "RecursionError"
	}
	return PythonClass(err)
}

func recordedError(err error) any {
	o := jsonx.Obj("class", recordedClass(err), "text", err.Error())
	var remote *RemoteRuleValidationError
	if errors.As(err, &remote) {
		o.Set("errors", remote.Errors)
	}
	return o
}

func noneString(v any) string {
	if !jsonx.Truthy(v) {
		return ""
	}
	return jsonx.Str(v)
}

// payloadFor derives the payload node Python passed in from the content (it
// came from read_rule_payload of the same content); the fingerprint check
// catches a call where it did not.
func payloadFor(content string, want any) (*pyyaml.Node, error) {
	if want == nil {
		return nil, nil
	}
	_, node, err := ReadRulePayload(content)
	if err != nil {
		return nil, err
	}
	if encode(nodeFingerprint(node)) != encode(want) {
		return nil, fmt.Errorf("payload node %s differs from %s", encode(nodeFingerprint(node)), encode(want))
	}
	return node, nil
}

func replayRecorded(row *jsonx.Object) (any, error) {
	args := row.GetObject("args")
	arg := func(name string) any { return recordedValue(args.Value(name)) }
	str := func(name string) string { s, _ := args.Value(name).(string); return s }
	obj := func(name string) *jsonx.Object { o, _ := args.Value(name).(*jsonx.Object); return o }
	hostURL, _ := row.Value("host_url").(string)
	switch row.GetString("fn") {
	case "content_digest":
		if d := ContentDigest(arg("content")); d != "" {
			return d, nil
		}
		return nil, nil
	case "classical_line_error":
		if message := rules.ClassicalLineError(str("value")); message != "" {
			return message, nil
		}
		return nil, nil
	case "inspect_domain_content":
		return InspectDomainContent(args.Value("content"), args.Value("behavior"), args.Value("rule_format"))
	case "normalize_classical_lines":
		content, warnings, err := NormalizeClassicalLines(str("content"))
		return []any{content, warnings}, err
	case "normalize_mihomo_yaml":
		content, warnings := NormalizeMihomoYAML(str("content"))
		return []any{content, warnings}, nil
	case "normalize_mihomo_ruleset":
		content, warnings, err := NormalizeMihomoRuleset(str("content"), args.Value("behavior"), args.Value("rule_format"))
		return []any{content, warnings}, err
	case "validate_domain_content":
		return nil, ValidateDomainContent(obj("rule"), obj("previous"))
	case "validate_remote_rule_content":
		format, _ := args.Value("rule_format").(string)
		return nil, ValidateRemoteRuleContent(arg("content"), obj("rule"), obj("config_data"), format)
	case "read_rule_payload":
		lines, node, err := ReadRulePayload(str("content"))
		return []any{strs(lines), nodeFingerprint(node)}, err
	case "append_rule_lines", "append_rule_payload":
		payload, err := payloadFor(str("content"), args.Value("payload"))
		if err != nil {
			return nil, err
		}
		if row.GetString("fn") == "append_rule_payload" {
			return AppendRulePayload(str("content"), str("line"), payload), nil
		}
		return AppendRuleLines(str("content"), jsonx.Strings(args.GetList("lines")), payload), nil
	case "remove_rule_payload":
		answers := row.GetObject("matches")
		var unknown []string
		content, removed, err := RemoveRulePayload(str("content"), func(line string) bool {
			answer, ok := answers.Value(line).(bool)
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
		return CleanText(str("value"), int(args.GetInt("limit"))), nil
	case "_meta_content":
		return metaContent(str("value")), nil
	case "known_service":
		if s := KnownService(str("host")); s != nil {
			return s, nil
		}
		return nil, nil
	case "page_identity":
		return PageIdentity(obj("result"), str("host"), args.Value("checked_at")), nil
	case "resolve_remote_rule_source":
		source, err := ResolveRemoteRuleSource(str("url"), obj("config"), noneString(args.Value("source_id")))
		if source == nil {
			return nil, err
		}
		return source, err
	case "validate_rule_url":
		u, err := ValidateRuleURL(str("url"))
		return []any{u.Scheme, u.Netloc, u.Path, u.Query, u.Fragment}, err
	case "normalize_rule_fetch_proxy":
		return NormalizeRuleFetchProxy(args.Value("value"))
	case "is_internal_rule_url":
		return IsInternalRuleURLChecked(str("url"), obj("config_data"), hostURL)
	case "get_rule_fetch_proxy":
		return GetRuleFetchProxy(obj("config_data"))
	case "rule_request_options":
		proxy, err := RuleRequestProxyChecked(str("url"), obj("config_data"), hostURL)
		return jsonx.Obj("proxies", jsonx.Obj("http", proxy, "https", proxy)), err
	case "is_github_url":
		return IsGithubURL(str("url")), nil
	case "github_download_candidates":
		candidates, err := GithubDownloadCandidates(str("url"), obj("config_data"), hostURL)
		var out []any
		for _, c := range candidates {
			out = append(out, []any{c.Label, c.URL})
		}
		return out, err
	}
	return nil, fmt.Errorf("unknown function %s", row.GetString("fn"))
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
	for sc.Scan() {
		value, err := jsonx.Parse(sc.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		rows++
		fn := row.GetString("fn")
		got, gotErr := replayRecorded(row)
		var gotText, wantText string
		if want, isErr := row.Get("err"); isErr {
			wantText = encode(want)
			if gotErr != nil {
				gotText = encode(recordedError(gotErr))
			} else {
				gotText = "ok " + encode(got)
			}
		} else {
			wantText = encode(row.Value("ok"))
			if gotErr != nil {
				gotText = "error " + encode(recordedError(gotErr))
			} else {
				gotText = encode(got)
			}
		}
		if gotText != wantText {
			failures[fn]++
			if failures[fn] <= 3 {
				t.Errorf("%s (%s)\nargs %.600s\n got %.800s\nwant %.800s", fn, row.GetString("test"),
					encode(row.Value("args")), gotText, wantText)
			}
		}
	}
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
