package rulefetch

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// pyClass names the Python exception class an error stands for.
func pyClass(err error) string {
	var remote *RemoteRuleValidationError
	switch {
	case errors.As(err, &remote):
		return "RemoteRuleValidationError"
	case errors.Is(err, repo.ErrValidation):
		return "ProfileValidationError"
	}
	return "ValueError"
}

func errorValue(err error) any {
	var errs any
	var remote *RemoteRuleValidationError
	if errors.As(err, &remote) {
		errs = remote.Errors
	}
	return jsonx.Obj("class", pyClass(err), "text", err.Error(), "errors", errs)
}

func encode(v any) string {
	out, err := jsonx.Encode(v, jsonx.Options{Compact: true})
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(out)
}

func strs(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func runCorpusRow(fn string, args []any) (any, error) {
	str := func(i int) string { s, _ := args[i].(string); return s }
	obj := func(i int) *jsonx.Object { o, _ := args[i].(*jsonx.Object); return o }
	switch fn {
	case "inspect":
		return InspectDomainContent(args[0], args[1], args[2])
	case "normalize_classical":
		content, warnings, err := NormalizeClassicalLines(str(0))
		return []any{content, warnings}, err
	case "normalize_yaml":
		content, warnings := NormalizeMihomoYAML(str(0))
		return []any{content, warnings}, nil
	case "normalize_ruleset":
		content, warnings, err := NormalizeMihomoRuleset(str(0), args[1], args[2])
		return []any{content, warnings}, err
	case "validate_remote":
		return nil, ValidateRemoteRuleContent(args[0], obj(1), nil, "")
	case "validate_remote_bytes":
		data, _ := base64.StdEncoding.DecodeString(str(0))
		return nil, ValidateRemoteRuleContent(data, obj(1), nil, "")
	case "read_payload":
		lines, _, err := ReadRulePayload(str(0))
		return strs(lines), err
	case "append":
		_, payload, err := ReadRulePayload(str(0))
		if err != nil {
			return nil, err
		}
		return AppendRuleLines(str(0), []string{"+.new.test", `x"y`}, payload), nil
	case "remove":
		content, removed, err := RemoveRulePayload(str(0), func(line string) bool {
			return containsRune(line, 'x') || line == "example.com"
		})
		return []any{content, strs(removed)}, err
	case "validate_domain":
		return nil, ValidateDomainContent(obj(0), nil)
	case "site_title":
		return SiteTitle(str(0)), nil
	case "clean_text":
		return CleanText(str(0), 200), nil
	case "known_service":
		if s := KnownService(str(0)); s != nil {
			return s, nil
		}
		return nil, nil
	case "page_identity":
		return PageIdentity(obj(0), "example.com", "now"), nil
	case "resolve_source":
		source, err := ResolveRemoteRuleSource(str(0), obj(1), str(2))
		if source == nil {
			return nil, err
		}
		return source, err
	case "validate_rule_url":
		u, err := ValidateRuleURL(str(0))
		return []any{u.Scheme, u.Netloc, u.Path, u.Query, u.Fragment}, err
	case "normalize_proxy":
		return NormalizeRuleFetchProxy(args[0])
	case "urljoin":
		return URLJoin(str(0), str(1))
	}
	panic("unknown fn " + fn)
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// TestCorpus replays testdata/corpus.jsonl.gz (testdata/gen.py, real Python).
func TestCorpus(t *testing.T) {
	f, err := os.Open("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	rows, failures := 0, map[string]int{}
	for sc.Scan() {
		value, err := jsonx.Parse(sc.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		rows++
		fn := row.GetString("fn")
		got, gotErr := runCorpusRow(fn, row.GetList("args"))
		var gotText, wantText string
		if want, isErr := row.Get("err"); isErr {
			wantText = encode(want)
			if gotErr != nil {
				gotText = encode(errorValue(gotErr))
			} else {
				gotText = "ok " + encode(got)
			}
		} else {
			wantText = encode(row.Value("ok"))
			if gotErr != nil {
				gotText = "error " + encode(errorValue(gotErr))
			} else {
				gotText = encode(got)
			}
		}
		if gotText != wantText {
			failures[fn]++
			if failures[fn] <= 4 {
				t.Errorf("%s%s\n got %s\nwant %s", fn, encode(row.Value("args")), gotText, wantText)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Errorf("failures by function: %v (of %d rows)", failures, rows)
	}
	if rows < 10000 {
		t.Fatalf("only %d rows", rows)
	}
}
