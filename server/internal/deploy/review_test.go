package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/yamlx"
)

// reviewCorpus is testdata/review.json, written by gen_a11_review.py from the
// real Python functions (A11 HR review findings F1–F5).
type reviewCorpus struct {
	Conversions []struct {
		Text, Kind, Result, Error string
		Mixed                     bool
	}
	Excerpts []struct {
		Text   string
		Line   int
		Result string
	}
	Geo []struct {
		Text     string
		Files    map[string]string
		Required []string
	}
	Fingerprints []struct {
		Manifest    json.RawMessage
		Fingerprint string
	}
	Builds []struct {
		Name, Service, Main, Error string
		Remedy                     *Remedy
		Downloads                  []struct {
			Name, URL, Behavior, Format string
			LocalPath                   string `json:"local_path"`
		}
		Files map[string]string
	}
	Payloads map[string]string
}

func loadReviewCorpus(t *testing.T) reviewCorpus {
	t.Helper()
	data, err := os.ReadFile("testdata/review.json")
	if err != nil {
		t.Fatal(err)
	}
	return decodeReviewCorpus(t, data)
}

func decodeReviewCorpus(t *testing.T, data []byte) reviewCorpus {
	t.Helper()
	var corpus reviewCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

// pythonKeyError is the KeyError text Python appends when a JSON rule document
// has no payload key; Go refuses the same input with the same Chinese message
// and no Python exception text (PORTING: internal exception texts not chased).
const pythonKeyError = "\n原始诊断：'payload'"

// F1: quoted payload keys and flow/JSON mappings are YAML rule sets; a
// non-payload JSON document is refused instead of becoming an empty rule file.
func TestReviewYAMLPayloadForms(t *testing.T) {
	for i, row := range loadReviewCorpus(t).Conversions {
		got, err := ConvertMosDNSRules([]byte(row.Text), row.Kind, row.Mixed)
		if row.Error != "" {
			if err == nil {
				t.Errorf("row %d %q %s mixed=%v: accepted %q, Python refused: %s", i, row.Text, row.Kind, row.Mixed, got, row.Error)
			} else if want := strings.TrimSuffix(row.Error, pythonKeyError); err.Error() != want {
				t.Errorf("row %d error\n%s\nwant\n%s", i, err, want)
			}
			continue
		}
		want, _ := base64.StdEncoding.DecodeString(row.Result)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("row %d %q %s mixed=%v: got %q %v want %q", i, row.Text, row.Kind, row.Mixed, got, err, want)
		}
	}
}

// F2: a credential whose value continues on the excerpted line stays hidden.
func TestReviewExcerptHidesContinuedCredentials(t *testing.T) {
	for _, row := range loadReviewCorpus(t).Excerpts {
		if got := SourceExcerpt(row.Text, row.Line); got != row.Result {
			t.Errorf("excerpt %q line %d got %q want %q", row.Text, row.Line, got, row.Result)
		}
	}
}

// F3: YAML booleans and numeric zeros decide Geo requirements by value.
func TestReviewGeoSelectionUsesYAMLValues(t *testing.T) {
	for _, row := range loadReviewCorpus(t).Geo {
		doc, err := yamlx.Parse([]byte(row.Text))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]Content{}
		for p, v := range row.Files {
			files[p] = Content{[]byte(v), RoleRule}
		}
		got := RequiredGeoResources(doc, files)
		if len(got) == 0 && len(row.Required) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, row.Required) {
			t.Errorf("%q: got %v want %v", row.Text, got, row.Required)
		}
	}
}

// F4: the persisted v1 fingerprint is byte-identical for every valid path.
func TestReviewFingerprintEscapes(t *testing.T) {
	for _, row := range loadReviewCorpus(t).Fingerprints {
		var m Manifest
		if err := json.Unmarshal(row.Manifest, &m); err != nil {
			t.Fatal(err)
		}
		got, err := Fingerprint(m)
		if err != nil || got != row.Fingerprint {
			t.Errorf("%q: got %q %v want %q", m.Files[1].Path, got, err, row.Fingerprint)
		}
	}
}

// F5: a receipt whose profile snapshot is larger than 32 MiB still loads.
func TestReviewLargeReceiptSourceLoads(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 40<<20)
	if _, err := readPrivate(writeTemp(t, data), maxReceiptBytes); err != nil {
		t.Fatal(err)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := t.TempDir() + "/receipt.json"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// F1 at the Build boundary, plus the Chinese download-failure texts and
// remedies of declared, undeclared, library and Mihomo sources.
func TestReviewBuildDownloadsAndJSONSources(t *testing.T) {
	raw, err := os.ReadFile("testdata/review.json")
	if err != nil {
		t.Fatal(err)
	}
	payloads := decodeReviewCorpus(t, raw).Payloads
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(text))
	}))
	defer srv.Close()
	corpus := decodeReviewCorpus(t, bytes.ReplaceAll(raw, []byte("@RULE_SERVER@"), []byte(srv.URL)))
	source := map[string]any{"profile_id": "alpha", "system_config": map[string]any{"server_domain": "https://panel.example"},
		"rule_library": []any{map[string]any{"id": "lib", "name": "Library", "source_type": "url", "url": srv.URL + "/missing", "behavior": "domain", "format": "text"}}}
	for _, row := range corpus.Builds {
		t.Run(row.Name, func(t *testing.T) {
			in := BuildInput{Target: Target{ID: "agent-example", ServiceType: row.Service, ProfileID: "alpha"}, Source: testSource(t, source), ConfigContent: row.Main, DeploymentID: "a11-review", BaseURL: "https://panel.example"}
			for _, d := range row.Downloads {
				in.RulesetDownloads = append(in.RulesetDownloads, convert.Download{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: d.Behavior, Format: d.Format})
			}
			b, err := Build(testContext(t), in)
			if row.Error != "" {
				if err == nil {
					t.Fatal("accepted", b.Manifest())
				}
				// The raw transport line holds Python's exception text (not chased).
				want, _, _ := strings.Cut(row.Error, "\n原始错误：")
				want = strings.TrimSuffix(want, pythonKeyError)
				got, _, _ := strings.Cut(err.Error(), "\n原始错误：")
				if got != want {
					t.Fatalf("error\n%s\nwant\n%s", got, want)
				}
				var prep *PreparationError
				var download *RuleDownloadError
				var remedy *Remedy
				if errors.As(err, &download) {
					remedy = download.Remedy
				} else if errors.As(err, &prep) {
					remedy = prep.Remedy
				}
				if !reflect.DeepEqual(remedy, row.Remedy) {
					t.Fatalf("remedy %+v want %+v", remedy, row.Remedy)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			files, err := b.Files()
			if err != nil {
				t.Fatal(err)
			}
			for p, encoded := range row.Files {
				want, _ := base64.StdEncoding.DecodeString(encoded)
				if !bytes.Equal(files[p], want) {
					t.Errorf("%s: %q want %q", p, files[p], want)
				}
			}
		})
	}
}

// F7: reading a few files of an opened (validated, immutable) bundle reads
// only up to the wanted members, like Python's _archive_files.
func TestReviewFilesStopsAtWantedMembers(t *testing.T) {
	b := sample(t, "mihomo", 3)
	archive, err := b.Archive()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(b.Manifest(), archive, digest(archive), b.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Swap in a stream whose bytes after files/config.yaml are not gzip at all.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, member := range []struct {
		name string
		data []byte
	}{{"manifest.json", []byte("{}")}, {"files/config.yaml", []byte("plugins: []\n")}} {
		if err = tw.WriteHeader(&tar.Header{Name: member.name, Size: int64(len(member.data)), Mode: 0o600, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	opened.archive = append(buf.Bytes(), "not a gzip member"...)
	files, err := opened.Files("config.yaml")
	if err != nil || string(files["config.yaml"]) != "plugins: []\n" {
		t.Fatalf("%q %v", files, err)
	}
	if _, err = opened.Files("rules/中文😀.txt"); err == nil {
		t.Fatal("reading past the wanted members did not reach the damaged stream")
	}
}

// F9: a rule download failure is a preparation failure for errors.As, and
// still reaches its transport cause.
func TestReviewRuleDownloadErrorIsPreparationError(t *testing.T) {
	cause := errors.New("synthetic transport failure")
	var err error = &RuleDownloadError{&PreparationError{Message: "download failed", Remedy: &Remedy{Kind: "rule_download"}, cause: cause}}
	var prep *PreparationError
	if !errors.As(err, &prep) || prep.Remedy == nil || prep.Message != "download failed" {
		t.Fatalf("not a preparation error: %#v", prep)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause lost")
	}
}
