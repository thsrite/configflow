package pushdiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

func aliasChain() string {
	s := "a0: &a0 [value]\n"
	for i := 1; i <= 40; i++ {
		s += fmt.Sprintf("a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	return s
}
func scalarAliases() string {
	s := "a0: &a0 {value: unchanged}\n"
	for i := 1; i <= 40; i++ {
		s += fmt.Sprintf("a%d: &a%d {value: *a%d}\n", i, i, i-1)
	}
	return s
}
func TestYAMLEntrypoints(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		accepted     bool
	}{
		{"depth-40-expansion", aliasChain(), false}, {"depth-40-linear", scalarAliases(), true},
		{"self-reference", "a: &a [*a]\n", false}, {"duplicate", "a: 1\na: 2\n", false},
		{"multiple-documents", "a: 1\n---\na: 2\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := Config([]byte(tc.source), []byte(tc.source), "", "")
			if (config.Status != "unparsable") != tc.accepted {
				t.Fatalf("config: %+v", config)
			}
			// All YAML entries must pass the same facade before navigating a subtree.
			provider := tc.source + "proxies: []\n"
			_, err := nodeItems([]byte(provider))
			if (err == nil) != tc.accepted {
				t.Fatalf("node input: %v", err)
			}
			rules := tc.source + "payload: []\n"
			_, err = RuleEntries([]byte(rules))
			if (err == nil) != tc.accepted {
				t.Fatalf("rules input: %v", err)
			}
		})
	}
}

func TestRulePayloadForms(t *testing.T) {
	for _, source := range []string{"payload: ['+.a.example.com', b.example.com]\n", `{"payload": ["+.a.example.com", "b.example.com"]}`, "'payload':\n- '+.a.example.com' # comment\n- \"b.example.com\"\n", "payload: &p\n- '+.a.example.com'\n- b.example.com\n"} {
		t.Run(source, func(t *testing.T) {
			got, err := RuleEntries([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			compareJSON(t, got, []string{"+.a.example.com", "b.example.com"})
		})
	}
	for _, source := range []string{"payload: scalar", "payload: [{nested: item}]", "payload: [*missing]", "payload: [a]\npayload: [b]"} {
		if _, err := RuleEntries([]byte(source)); err == nil {
			t.Fatalf("accepted %q", source)
		}
	}
	got, err := RuleEntries([]byte("\ufeff# comment\r\n// comment\r+.a.example.com\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	compareJSON(t, got, []string{"+.a.example.com"})
}

func TestScalarSemanticsAndMerges(t *testing.T) {
	result := Config([]byte("flag: False\nzero: 0x0\nnumber: 1\n"), []byte("flag: false\nzero: 0\nnumber: 1.0\n"), "", "")
	if len(result.Items) != 1 || result.Items[0].Key != "number" {
		t.Fatalf("semantic types: %+v", result.Items)
	}
	result = Config([]byte("base: &b {1: old, x: same}\nk: {<<: *b}\n"), []byte("base: &b {1: new, x: same}\nk: {<<: *b}\n"), "", "")
	if len(result.Items) != 2 || result.Items[1].Key != "k.1" {
		t.Fatalf("non-string merged key dropped: %+v", result.Items)
	}
}

func TestUnifiedPatchAndMarkers(t *testing.T) {
	sources := [][2]string{
		{"a: 1\nb: [x, y]\n", "a: 2\nb: [y, z]\n"},
		{"mode: rule\n", "password: |\n  private-line-one\n  private-line-two\nnext: visible\n"},
		{"a: &value old\nb: *value\n", "a: &value topsecret\nauth: *value\n"},
		{"a: old\n", "auth: {user: nested-user, pass: nested-password}\npassword: !custom tagged-password\nurl: https://user:pass@rules.example.com/path?token=token-value&flag=clash\ndialer: user:p'ass@192.0.2.1:1080\n"},
		{"a: old\n", "private-key: |\n  -----BEGIN PRIVATE KEY-----\n  synthetic-private-key\n  -----END PRIVATE KEY-----\nurl: \"tab\\t https://u:encoded-secret@rules.example.com/path\"\n"},
	}
	for i, pair := range sources {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			a, err := yamlx.Parse([]byte(pair[0]))
			if err != nil {
				t.Fatal(err)
			}
			b, err := yamlx.Parse([]byte(pair[1]))
			if err != nil {
				t.Fatal(err)
			}
			old, err := canonicalYAML(agentPaths(a))
			if err != nil {
				t.Fatal(err)
			}
			new, err := canonicalYAML(agentPaths(b))
			if err != nil {
				t.Fatal(err)
			}
			result := Config([]byte(pair[0]), []byte(pair[1]), "old.yaml", "new.yaml")
			if result.Raw == nil || result.Raw.Truncated {
				t.Fatalf("raw: %+v", result.Raw)
			}
			applyPatch(t, old, new, result.Raw.Lines)
			hidden, _ := json.Marshal(WithoutSecrets(result))
			if i == 2 {
				hidden, _ = json.Marshal(WithoutSecrets(result.Raw))
			}
			for _, secret := range []string{"private-line-one", "private-line-two", "topsecret", "nested-user", "nested-password", "tagged-password", "token-value", "p'ass", "synthetic-private-key", "BEGIN PRIVATE KEY", "encoded-secret"} {
				if bytes.Contains(new, []byte(secret)) && bytes.Contains(hidden, []byte(secret)) {
					t.Fatalf("secret %q visible: %s", secret, hidden)
				}
			}
			if i == 3 && !bytes.Contains(hidden, []byte("rules.example.com/path")) {
				t.Fatalf("plain URL context hidden: %s", hidden)
			}
		})
	}
}

func applyPatch(t *testing.T, old, new []byte, lines []Text) {
	t.Helper()
	before := strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")
	after := strings.Split(strings.TrimSuffix(string(new), "\n"), "\n")
	out := []string{}
	cursor := 0
	start := 0
	for i, line := range lines {
		text, _ := line.Unpack()
		if i < 2 {
			continue
		}
		if strings.HasPrefix(text, "@@ ") {
			var count int
			_, err := fmt.Sscanf(text, "@@ -%d,%d", &start, &count)
			if err != nil {
				_, err = fmt.Sscanf(text, "@@ -%d ", &start)
				if err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(text, "@@ -0,0") {
				start = 0
			} else {
				start--
			}
			if start < cursor {
				t.Fatalf("overlapping hunk %s", text)
			}
			out = append(out, before[cursor:start]...)
			cursor = start
			continue
		}
		if text == "" {
			t.Fatal("empty diff operation")
		}
		switch text[0] {
		case ' ':
			if cursor >= len(before) || before[cursor] != text[1:] {
				t.Fatalf("invalid context %q", text)
			}
			out = append(out, text[1:])
			cursor++
		case '-':
			if cursor >= len(before) || before[cursor] != text[1:] {
				t.Fatalf("invalid removal %q", text)
			}
			cursor++
		case '+':
			out = append(out, text[1:])
		default:
			t.Fatalf("invalid operation %q", text)
		}
	}
	out = append(out, before[cursor:]...)
	if !slices.Equal(out, after) {
		t.Fatalf("patch does not reproduce candidate\ngot %q\nwant %q", out, after)
	}
}

func TestRawAndValueBounds(t *testing.T) {
	result := Config([]byte("hosts: {}\n"), []byte("long: "+strings.Repeat("中", 5000)+"\nhosts:\n"+func() string {
		var b strings.Builder
		for i := 0; i < 3000; i++ {
			fmt.Fprintf(&b, "  host-%d.example.com: %s\n", i, strings.Repeat("x", 100))
		}
		return b.String()
	}()), "", "")
	if len(result.Items) != MaxConfigRows || *result.Total != 3001 || result.Omitted != 2801 {
		t.Fatalf("rows %+v", result)
	}
	text, _ := result.Items[0].New.Unpack()
	if !strings.HasSuffix(text, "（共 5000 字符）") {
		t.Fatalf("unicode clipping: %s", text)
	}
	raw := result.Raw
	size := 0
	for _, t := range raw.Lines {
		s, _ := t.Unpack()
		size += len(s) + 1
	}
	if !raw.Truncated || len(raw.Lines) > MaxRawLines || size > MaxRawBytes || raw.OmittedLines <= 0 {
		t.Fatalf("raw bounds %+v", raw)
	}
	huge := []byte("rules:\n" + strings.Repeat("- DOMAIN,example.com,DIRECT\n", MaxRawInputLines))
	r := Config([]byte("rules: []\n"), huge, "", "")
	if r.Raw.Reason != "too_large" {
		t.Fatalf("input bound %+v", r.Raw)
	}
}

func TestStoredRedactionAndSearch(t *testing.T) {
	find := func(s string) []redact.Range {
		r := []rune(s)
		needle := []rune("stored-secret")
		out := []redact.Range{}
		for i := 0; i+len(needle) <= len(r); i++ {
			if slices.Equal(r[i:i+len(needle)], needle) {
				out = append(out, redact.Range{Start: i, End: i + len(needle)})
			}
		}
		return out
	}
	before := []byte("proxies:\n- {name: 香港, type: ss, port: 443, password: needle-before}\n")
	after := []byte("proxies:\n- {name: 香港, type: ss, port: 8443, password: needle-after}\n- {name: Relay stored-secret, type: http, server: relay.example.com, port: 80}\n- {name: 'https://rules.example.com/?token=needle-token', type: ss}\n")
	keep := &keeper{left: MaxKeptBytes}
	_, d := nodeChange(before, after, &detailBudget{left: MaxDetailBytes}, keep)
	f := keep.last
	if f == nil || !d.Paging {
		t.Fatal("not kept")
	}
	for _, tc := range []struct {
		change, q string
		find      func(string) []redact.Range
		total     int
	}{
		{"modified", "香港", nil, 1}, {"modified", "8443", nil, 1}, {"modified", "password", nil, 1}, {"modified", "needle-after", nil, 0},
		{"added", "http", nil, 2}, {"added", "relay.example.com:80", nil, 1}, {"added", "needle-token", nil, 0},
		{"added", "stored-secret", nil, 1}, {"added", "stored-secret", find, 0}, {"added", "relay", find, 1},
	} {
		t.Run(tc.q, func(t *testing.T) {
			page := f.Page(tc.change, 0, 50, SearchQuery(tc.q), tc.find)
			if page.Total != tc.total {
				t.Fatalf("%+v", page)
			}
		})
	}
	page := f.Page("added", 0, 50, "", nil)
	clean := WithoutStored(page, find)
	encoded, _ := json.Marshal(clean)
	if bytes.Contains(encoded, []byte("stored-secret")) || !bytes.Contains(encoded, []byte("[REDACTED]")) {
		t.Fatalf("redaction %s", encoded)
	}
	compareJSON(t, WithoutStored(page, func(string) []redact.Range { return nil }), page)
	if SearchQuery(" \r香港\n A\x00B ") != "香港  a b" {
		t.Fatal(SearchQuery(" \r香港\n A\x00B "))
	}
	compareJSON(t, SearchQuery("Straße"), Query("strasse"))
}

func makeBundle(t *testing.T, files map[string]deploy.Content) *deploy.Bundle {
	t.Helper()
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "synthetic", AgentID: "synthetic", ProfileID: "alpha", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	for p, v := range files {
		sum := sha256.Sum256(v.Data)
		m.Files = append(m.Files, deploy.File{Path: p, Role: v.Role, Size: int64(len(v.Data)), SHA256: hex.EncodeToString(sum[:])})
	}
	slices.SortFunc(m.Files, func(a, b deploy.File) int { return strings.Compare(a.Path, b.Path) })
	b, err := deploy.Pack(m, files, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestBundleLimitsAndImmutability(t *testing.T) {
	oldFiles := map[string]deploy.Content{"config.yaml": {Data: []byte("mode: rule\n"), Role: deploy.RoleConfig}}
	newFiles := map[string]deploy.Content{"config.yaml": {Data: []byte("mode: global\n"), Role: deploy.RoleConfig}}
	for i := 0; i < 600; i++ {
		p := fmt.Sprintf("rules/%04d", i)
		oldFiles[p] = deploy.Content{Data: []byte("+.old.example.com\n"), Role: deploy.RoleRule}
		newFiles[p] = deploy.Content{Data: []byte("+.new.example.com\n"), Role: deploy.RoleRule}
	}
	old, new := makeBundle(t, oldFiles), makeBundle(t, newFiles)
	digests, err := deploy.Digests(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	original := old.Manifest()
	receipt := &deploy.Receipt{Manifest: old.Manifest(), Bundle: old}
	d, err := Build(AppliedView{Files: digests}, new, receipt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != MaxListedFiles || d.FilesOmitted != 101 || d.Counts.Modified != 601 {
		t.Fatalf("file bounds %+v", d.Counts)
	}
	if !reflect.DeepEqual(original, old.Manifest()) || !bytes.Equal(oldFiles["config.yaml"].Data, []byte("mode: rule\n")) {
		t.Fatal("mutated inputs")
	}
	// Packed archives and in-memory bundles expose the same content without
	// eagerly reading unchanged or omitted files.
	if _, err := old.Archive(); err != nil {
		t.Fatal(err)
	}
	after, err := Build(AppliedView{Files: digests}, new, receipt, nil)
	if err != nil {
		t.Fatal(err)
	}
	compareJSON(t, after, d)
}

func TestPagingBudgetsAndConcurrency(t *testing.T) {
	old := []byte("+.old.example.com\n")
	var b strings.Builder
	for i := 0; i < MaxKeptEntries+1000; i++ {
		fmt.Fprintf(&b, "+.new-%06d.example.com\n", i)
	}
	keep := &keeper{left: MaxKeptBytes}
	_, d := ruleChange(old, []byte(b.String()), &detailBudget{left: 1}, keep)
	if !d.Limited || !d.Paging {
		t.Fatalf("details %+v", d)
	}
	f := keep.last
	if *d.Groups[0].Available != MaxKeptEntries-1 || *d.Groups[1].Available != 1 {
		t.Fatalf("unfair quotas %+v", d.Groups)
	}
	if f.Size() >= len(b.String()) {
		t.Fatal("not compressed")
	}
	first := f.Page("added", 0, 9999, "", nil)
	if len(first.Entries) != MaxPageSize {
		t.Fatal("page clamp")
	}
	end := f.Page("added", MaxKeptEntries, 50, "", nil)
	if len(end.Entries) != 0 || end.NextOffset != nil {
		t.Fatal("end offset")
	}
	// A comparison whose byte budget is spent still returns exact counts, and
	// never advertises paging for a file with no retained entries.
	keep = &keeper{left: 1}
	_, d = ruleChange(old, []byte(b.String()), &detailBudget{left: MaxDetailBytes}, keep)
	if d.Paging || keep.last != nil {
		t.Fatal("empty keeper advertised")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page := f.Page("added", 20, 50, SearchQuery("new-000"), nil)
			if len(page.Entries) != 50 {
				t.Errorf("concurrent read: %+v", page)
			}
		}()
	}
	wg.Wait()
	// Page byte ceiling always permits at least one large entry.
	writer := newWriter(10, MaxKeptFileBytes)
	large := Entry{Change: "added", Text: plain(strings.Repeat("x", MaxPageBytes))}
	writer.add(large)
	writer.add(large)
	big := &FileEntries{groups: map[string]storedGroup{"added": writer.close()}}
	page := big.Page("added", 0, 50, "", nil)
	if len(page.Entries) != 1 || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatalf("page byte bound %+v", page)
	}
}

func TestStoredRangeUnionAndUnicode(t *testing.T) {
	var value Text
	value.add("中文 prefix ", false)
	value.add("whole-stored-secret-tail", true)
	value.add(" suffix", false)
	cleaned := value.withoutStored(func(s string) []redact.Range {
		at := strings.Index(s, "stored-secret")
		start := len([]rune(s[:at]))
		return []redact.Range{{Start: start, End: start + len("stored-secret")}}
	})
	compareJSON(t, cleaned, []any{"中文 prefix ", map[string]string{"secret": "[REDACTED]"}, " suffix"})
	text, ranges := cleaned.Unpack()
	if text != "中文 prefix [REDACTED] suffix" || len(ranges) != 1 || ranges[0].Start != 10 {
		t.Fatalf("rune offsets %q %+v", text, ranges)
	}
	compareJSON(t, WithoutStored(map[string]any{"text": value}, nil), map[string]any{"text": value})
}

func TestFormattingNotes(t *testing.T) {
	old := []byte("mode: rule\n")
	for _, new := range [][]byte{[]byte(strings.Repeat("\n", 600) + string(old)), []byte("# " + strings.Repeat("comment ", 100) + "\nmode: rule\n")} {
		result := Config(old, new, "", "")
		if result.Status != "formatting" || result.Reformatted == nil || !*result.Reformatted {
			t.Fatalf("formatting note %+v", result)
		}
	}
}

func TestDeepConfigPathsRemainComplete(t *testing.T) {
	source := func(value string) []byte {
		var b strings.Builder
		for i := 0; i < 80; i++ {
			fmt.Fprintf(&b, "%sk%d:\n", strings.Repeat(" ", i*2), i)
		}
		fmt.Fprintf(&b, "%sleaf: %s\n", strings.Repeat(" ", 160), value)
		return []byte(b.String())
	}
	result := Config(source("before"), source("after"), "", "")
	if len(result.Items) != 1 || result.Items[0].Path == nil || len(*result.Items[0].Path) != 81 || !strings.HasSuffix(result.Items[0].Key, ".leaf") {
		t.Fatalf("deep path truncated: %+v", result.Items)
	}
	text, _ := result.Items[0].New.Unpack()
	if text != "after" {
		t.Fatal(text)
	}
}
