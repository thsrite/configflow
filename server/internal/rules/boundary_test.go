package rules

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Boundary corpus (testdata/boundary.jsonl.gz from testdata/boundary_gen.py,
// CPython 3.11 with the production modules): realistic node names (CJK,
// emoji, full-width, mixed scripts), node filters, DOMAIN-REGEX values,
// rule values of every type, rule lines, queries and host names. Every
// outcome must match, except the accepted classes of differences_test.go.

type boundaryRecord struct {
	Kind     string          `json:"kind"`
	Pattern  string          `json:"pattern"`
	Ignore   bool            `json:"ignore"`
	Error    *string         `json:"error"`
	Hits     []bool          `json:"hits"`
	Outputs  []string        `json:"outputs"`
	Include  json.RawMessage `json:"include"`
	Exclude  json.RawMessage `json:"exclude"`
	Kept     []int           `json:"kept"`
	RuleKind string          `json:"rule_kind"`
	Value    string          `json:"value"`
	Line     string          `json:"line"`
	Result   json.RawMessage `json:"result"`
	Query    string          `json:"query"`
	RuleType string          `json:"rule_type"`
	Host     string          `json:"host"`
	IDNA     *string         `json:"idna"`
}

// boundaryNames must stay equal to NAMES in boundary_gen.py.
var boundaryNames = []string{
	"🇭🇰 香港 01", "🇭🇰 香港 02 | IPLC", "🇯🇵 日本 东京 01", "🇯🇵 Japan Tokyo 02", "🇺🇸 美国 洛杉矶",
	"🇸🇬 新加坡 01 [0.5x]", "🇹🇼 台湾 01", "🇰🇷 韩国 首尔", "HK-01", "hk 02", "HKG|BGP|x1.5", "US-Seattle-03",
	"Singapore 04", "sg5", "JP_Osaka_06", "剩余流量：120.5 GB", "套餐到期：2026-12-31", "过期时间 2026-12-31",
	"官网 https://example.test", "距离下次重置剩余：12 天", "香港ＩＰＬＣ０１", "ＨＫ　０２", "日本０３（中转）",
	"🇩🇪 德国 法兰克福", "Germany 01 ✈", "UK London", "🇬🇧 英国 伦敦 02", "Россия 01", "Türkiye 01",
	"Ελλάδα 01", "العربية 01", "香港 01 - 游戏", "香港 Netflix 解锁", "HK 01 x2", "HK 01 ×0.1", "ChatGPT 美国",
	"DIRECT", "REJECT", "Auto 自动选择", "♻️ 自动选择", "🚀 节点选择", "node.example.test:443", "",
	"新加坡⁰¹", "台湾 ① 号", "HK01-备用", "香港 01 (v6)", "IPv6 香港", "Emoji 😀 only", "tab\tname",
}

func loadBoundary(t *testing.T) []boundaryRecord {
	t.Helper()
	file, err := os.Open("testdata/boundary.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	var records []boundaryRecord
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, 16<<20)
	for scanner.Scan() {
		var record boundaryRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

// jsonValue decodes an optional filter value (null, string or another JSON value).
func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	value, _ := jsonx.Parse(raw)
	return value
}

func TestBoundaryCorpus(t *testing.T) {
	counts := map[string]int{}
	for _, r := range loadBoundary(t) {
		counts[r.Kind]++
		switch r.Kind {
		case "search":
			re, err := CompilePyRegex(r.Pattern, r.Ignore)
			if r.Error != nil {
				if regexMoreLenient(true, err) {
					counts["search lenient"]++
					continue
				}
				if err == nil || !IsPyRegexError(err) {
					t.Errorf("compile %q: Go %v, Python %q", r.Pattern, err, *r.Error)
				}
				continue
			}
			if err != nil {
				if !IsPyRegexError(err) { // \N{name}: valid, not applied
					counts["search unsupported"]++
					continue
				}
				t.Errorf("compile %q (ignore=%v): %v, Python accepts", r.Pattern, r.Ignore, err)
				continue
			}
			for i, name := range boundaryNames {
				if re.Search(name) != r.Hits[i] && !(r.Ignore && turkishI(name)) {
					t.Errorf("search %q in %q (ignore=%v) = %v, Python %v", r.Pattern, name, r.Ignore, !r.Hits[i], r.Hits[i])
				}
			}
		case "sub":
			re, err := CompilePyRegex(r.Pattern, false)
			if err != nil {
				continue // reported by the search records
			}
			for i, name := range boundaryNames {
				got, err := re.Sub(name, func(m *Match) (string, error) {
					whole, _ := m.Group(0)
					return "<" + strings.ToUpper(whole) + ">", nil
				}, 0)
				if err != nil || got != r.Outputs[i] {
					t.Errorf("sub %q on %q = %q, %v; Python %q", r.Pattern, name, got, err, r.Outputs[i])
				}
			}
		case "filter":
			var nodes []any
			for _, name := range boundaryNames {
				nodes = append(nodes, jsonx.Obj("name", name))
			}
			kept, err := FilterCandidates(nodes, jsonValue(r.Include), jsonValue(r.Exclude))
			if r.Error != nil {
				if err == nil || err.Error() != *r.Error {
					t.Errorf("filter_candidates(%s, %s) = %v, Python %q", r.Include, r.Exclude, err, *r.Error)
				}
				continue
			}
			if err != nil {
				t.Errorf("filter_candidates(%s, %s): %v", r.Include, r.Exclude, err)
				continue
			}
			got := []int{}
			for _, item := range kept {
				for i, name := range boundaryNames {
					if item.(*jsonx.Object).GetString("name") == name {
						got = append(got, i)
						break
					}
				}
			}
			if gotText, wantText := jsonText(t, got), jsonText(t, r.Kept); gotText != wantText {
				t.Errorf("filter_candidates(%s, %s) kept %s, Python %s", r.Include, r.Exclude, gotText, wantText)
			}
		case "rve":
			want := ""
			if r.Error != nil {
				want = *r.Error
			}
			got := RuleValueError(r.RuleKind, r.Value)
			if strings.HasPrefix(want, regexInvalid) && strings.HasPrefix(got, regexInvalid) {
				continue // the detail is the engine's error text
			}
			if got != want {
				t.Errorf("rule_value_error(%q, %q) = %q, Python %q", r.RuleKind, r.Value, got, want)
			}
		case "line":
			kind, value, ok := ParseRuleLine(r.Line)
			var got any
			if ok {
				got = []string{kind, value}
			}
			if gotText := jsonText(t, got); gotText != normalized(t, r.Result) {
				t.Errorf("parse_rule_line(%q) = %s, Python %s", r.Line, gotText, r.Result)
			}
		case "query":
			if got := MatchQuery(r.Query, r.RuleType, r.Value); jsonText(t, got) != normalized(t, r.Result) {
				t.Errorf("match_query(%q, %s, %q) = %v, Python %s", r.Query, r.RuleType, r.Value, got, r.Result)
			}
		case "domain":
			want := ""
			if r.Error != nil {
				want = *r.Error
			}
			if got := DomainValueError(r.Host); got != want {
				t.Errorf("domain_value_error(%q) = %q, Python %q", r.Host, got, want)
			}
			encoded, ok := IDNAEncode(r.Host)
			if ok != (r.IDNA != nil) || ok && !strings.EqualFold(encoded, *r.IDNA) {
				t.Errorf("idna(%q) = %q %v, Python %v", r.Host, encoded, ok, r.IDNA)
			}
		}
	}
	t.Logf("records: %v", counts)
}

func jsonText(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func normalized(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return jsonText(t, value)
}
