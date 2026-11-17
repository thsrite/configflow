package deploy

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// seedTexts returns the rule texts of the Python corpora (rules.json, review.json).
func seedTexts(f *testing.F) []string {
	f.Helper()
	var out []string
	for _, name := range []string{"testdata/rules.json", "testdata/review.json"} {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		var corpus struct {
			Rows, Conversions, Locations, Excerpts []struct{ Text string }
		}
		if err = json.Unmarshal(data, &corpus); err != nil {
			f.Fatal(err)
		}
		for _, rows := range [][]struct{ Text string }{corpus.Rows, corpus.Conversions, corpus.Locations, corpus.Excerpts} {
			for _, row := range rows {
				out = append(out, row.Text)
			}
		}
	}
	return out
}

// FuzzConvertMosDNSRules: any source converts or is refused, never panics, and
// a successful domain_set never carries a raw Clash rule or a blank line.
func FuzzConvertMosDNSRules(f *testing.F) {
	for _, text := range seedTexts(f) {
		f.Add([]byte(text), false, false)
		f.Add([]byte(text), true, true)
	}
	f.Fuzz(func(t *testing.T, content []byte, ip, mixed bool) {
		kind := "domain_set"
		if ip {
			kind = "ip_set"
		}
		out, err := ConvertMosDNSRules(content, kind, mixed)
		if err != nil {
			return
		}
		if !utf8.Valid(out) {
			t.Fatalf("invalid UTF-8 output %q", out)
		}
		if len(out) == 0 {
			return
		}
		for _, line := range strings.Split(string(out), "\n") {
			if line == "" || (!ip && strings.Contains(line, ",") && !strings.HasPrefix(line, "regexp:") && !strings.HasPrefix(line, "keyword:") && !strings.HasPrefix(line, "full:") && !strings.HasPrefix(line, "domain:")) {
				t.Fatalf("unexpected converted line %q in %q", line, out)
			}
		}
	})
}

// FuzzSourceExcerpt: every line of any text is excerpted without panicking,
// deterministically (credential hiding itself is compared with Python in
// TestPythonRulesAndLocations and TestReviewExcerptHidesContinuedCredentials).
func FuzzSourceExcerpt(f *testing.F) {
	for _, text := range seedTexts(f) {
		f.Add(text, 1)
		f.Add(text, 2)
	}
	f.Fuzz(func(t *testing.T, text string, line int) {
		got := SourceExcerpt(text, line)
		if again := SourceExcerpt(text, line); again != got {
			t.Fatalf("excerpt not deterministic: %q then %q", got, again)
		}
		_ = SourceError("Invalid rule YAML payload", text, Position{Line: &line}, nil, msgBeforeConversion)
	})
}

// FuzzOpenArchive: arbitrary bytes under a valid manifest are refused or read
// back exactly as listed; Open never panics and never accepts unlisted members.
func FuzzOpenArchive(f *testing.F) {
	b := sampleBundle(f)
	m := b.Manifest()
	archive, err := b.Archive()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(archive)
	f.Add(archive[:len(archive)/2])
	f.Add([]byte{0x1f, 0x8b})
	if python, err := os.ReadFile("testdata/python.tar.gz"); err == nil {
		f.Add(python)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		opened, err := Open(m, data, digest(data), b.ConfigVersion(), nil)
		if err != nil {
			return
		}
		files, err := opened.Contents()
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != len(m.Files) {
			t.Fatalf("%d files for %d entries", len(files), len(m.Files))
		}
		for _, entry := range m.Files {
			if digest(files[entry.Path].Data) != entry.SHA256 {
				t.Fatalf("%s differs from its manifest", entry.Path)
			}
		}
		if again, _ := opened.Archive(); !bytes.Equal(again, data) {
			t.Fatal("opened archive changed")
		}
	})
}

func sampleBundle(f *testing.F) *Bundle {
	f.Helper()
	files := map[string]Content{"config.yaml": {[]byte("plugins: []\n"), RoleConfig}, "rules/中文😀.txt": {[]byte("example.com\n"), RoleRule}, "empty.txt": {[]byte{}, RoleCustom}}
	m := Manifest{ProtocolVersion: 1, DeploymentID: "a11-fuzz", AgentID: "agent-example", ProfileID: "profile-example", ServiceType: "mosdns", ConfigPath: "config.yaml"}
	for _, p := range sortedContents(files) {
		m.Files = append(m.Files, File{Path: p, Size: int64(len(files[p].Data)), SHA256: digest(files[p].Data), Role: files[p].Role})
	}
	b, err := Pack(m, files, digest(files["config.yaml"].Data)[:8], nil)
	if err != nil {
		f.Fatal(err)
	}
	return b
}
