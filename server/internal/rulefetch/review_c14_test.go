package rulefetch

import (
	"bufio"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// reviewRow answers one row of testdata/review_c14.jsonl.gz.
func reviewRow(fn string, args []any) (any, error) {
	str := func(i int) string { s, _ := args[i].(string); return s }
	obj := func(i int) *jsonx.Object { o, _ := args[i].(*jsonx.Object); return o }
	switch fn {
	case "unescape":
		return pyUnescape(str(0)), nil
	case "meta_content":
		return metaContent(str(0)), nil
	case "clean_text":
		limit, _ := jsonx.AsInt(args[1])
		return CleanText(str(0), int(limit)), nil
	case "is_github_url":
		return IsGithubURL(str(0)), nil
	case "urlsplit_error":
		u, err := SplitURL(str(0), "")
		return []any{u.Scheme, u.Netloc, u.Path, u.Query, u.Fragment}, err
	case "is_internal_rule_url":
		return IsInternalRuleURLChecked(str(0), obj(1), "")
	case "github_download_candidates":
		candidates, err := GithubDownloadCandidates(str(0), obj(1), "")
		var out []any
		for _, c := range candidates {
			out = append(out, []any{c.Label, c.URL})
		}
		return out, err
	case "rule_request_options":
		proxy, err := RuleRequestProxyChecked(str(0), obj(1), "")
		return jsonx.Obj("proxies", jsonx.Obj("http", proxy, "https", proxy)), err
	case "page_identity":
		return PageIdentity(obj(0), "example.com", "now"), nil
	case "nfkc_forbidden":
		var out []any
		for code := rune(0x80); code < 0x110000; code++ {
			if code >= 0xD800 && code <= 0xDFFF {
				continue
			}
			if !validNetloc(string(code)) {
				out = append(out, jsonx.Int(int64(code)))
			}
		}
		return out, nil
	}
	return runCorpusRow(fn, args)
}

// reviewAccepted lists the rows whose difference is documented in PROGRESS
// (section C14, accepted differences).
func reviewAccepted(fn string, args []any, want string) bool {
	content, _ := args[0].(string)
	switch {
	case fn == "normalize_yaml" && (strings.Contains(content, "!!set") || strings.Contains(content, "!!omap")):
		// yamlx cannot hold !!set / !!omap values; Python re-dumps them.
		return true
	case fn == "append" && content == "payload: x\npayload: [a.com]\n":
		// A first payload key that is not a list: Python's append raises
		// AttributeError. Unreachable: validate_domain_content refuses a
		// document without a unique payload list before it is stored.
		return true
	}
	return false
}

// TestReviewC14Corpus replays testdata/review_c14.jsonl.gz
// (testdata/gen_review_c14.py, real CPython 3.11.17).
func TestReviewC14Corpus(t *testing.T) {
	f, err := os.Open("testdata/review_c14.jsonl.gz")
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
	rows, failures, accepted := 0, map[string]int{}, 0
	for sc.Scan() {
		value, err := jsonx.Parse(sc.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		rows++
		fn := row.GetString("fn")
		got, gotErr := reviewRow(fn, row.GetList("args"))
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
		if gotText == wantText {
			continue
		}
		if reviewAccepted(fn, row.GetList("args"), wantText) {
			accepted++
			continue
		}
		failures[fn]++
		if failures[fn] <= 4 {
			t.Errorf("%s%.400s\n got %.600s\nwant %.600s", fn, encode(row.Value("args")), gotText, wantText)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Errorf("differences by function: %v (of %d rows)", failures, rows)
	}
	t.Logf("%d rows, %d accepted differences", rows, accepted)
}
