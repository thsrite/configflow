package oxidns

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

type archiveInput struct {
	Config    json.RawMessage   `json:"config"`
	YAML      string            `json:"yaml"`
	Downloads []mosdns.Download `json:"downloads"`
	Files     []archiveFile     `json:"files"`
	BaseURL   string            `json:"base_url"`
}
type archiveFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}
type archiveAnswer struct {
	Files map[string]string `json:"files"`
	Names []string          `json:"names"`
	Modes map[string]uint32 `json:"modes"`
	Error string            `json:"error"`
	Class string            `json:"class"`
}

func TestC29ArchivePythonBoundary(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	respond := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			http.Error(w, "down", 503)
			return
		}
		if r.URL.Path == "/raw" {
			io.WriteString(w, "||ads.example^$dnstype=A\n@@||safe.example^\n")
			return
		}
		io.WriteString(w, "DOMAIN,exact.example\nDOMAIN-SUFFIX,suffix.example\nIP-CIDR,192.0.2.0/24\n")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		respond(w, r)
	}))
	defer upstream.Close()
	pythonUpstream := httptest.NewServer(http.HandlerFunc(respond))
	defer pythonUpstream.Close()
	oracle := testgolden.NewLines(t, "server/internal/oxidns/testdata/c29_archive.py")
	referenceFetches := map[string]int{}

	ctx := context.Background()
	raw := json.RawMessage(`{"profile_id":"default","system_config":{"server_domain":"http://config.example","rule_proxy_token":"c29-private"},"rule_configs":[{"library_rule_id":"inline","enabled":true}],"rule_library":[{"id":"inline","name":"Inline","source_type":"content","content":"DOMAIN,inline.example\nDOMAIN-SUFFIX,suffix.example","format":"text","behavior":"classical"}]}`)
	configs, err := mosdns.ConfigFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, text string
		downloads  []mosdns.Download
		files      []File
	}{
		{name: "empty", text: "plugins: []"},
		{name: "binary and nested resources", text: "plugins: [{type: geosite, args: {file: ./geo.dat}}, {type: ros_address_list, args: {persistent: {files: [rules/data.txt]}}}]", files: []File{{Path: "geo.dat", Content: []byte{0, 1, 255}}, {Path: "rules/data.txt", Content: []byte("example.com\n")}}},
		{name: "raw and repeated source", text: "plugins: [{args: {files: [a.txt, b.txt]}}]", downloads: []mosdns.Download{{URL: upstream.URL + "/raw", Name: "A", LocalPath: "a.txt"}, {URL: upstream.URL + "/raw", Name: "B", LocalPath: "b.txt"}}},
		{name: "local domain and IP callbacks", text: "plugins: [{args: {files: [domain.txt, ip.txt]}}]", downloads: []mosdns.Download{{URL: "http://config.example/api/mosdns/rule-proxy?url=http%3A%2F%2Fconfig.example%2Fapi%2Fprofiles%2Fdefault%2Frule-library%2Fcontent%2Finline&part=domain", Name: "Inline", LocalPath: "domain.txt"}, {URL: "http://config.example/api/mosdns/rule-proxy?url=http%3A%2F%2Fconfig.example%2Fapi%2Fprofiles%2Fdefault%2Frule-library%2Fcontent%2Finline&part=ip", Name: "Inline", LocalPath: "ip.txt"}}},
		{name: "missing file", text: "plugins: [{args: {files: [missing.txt]}}]"},
		{name: "missing singular file", text: "plugins: [{type: geosite, args: {file: missing.dat}}]"},
		{name: "missing nested file", text: "plugins: [{type: ros_address_list, args: {persistent: {files: [missing.txt]}}}]"},
		{name: "runtime excluded", text: "plugins: []", files: []File{{Path: "./state/learned.txt"}}},
		{name: "include excluded", text: "include: [parts/child.yaml]\nplugins: []", files: []File{{Path: "parts/child.yaml", Content: []byte("plugins: []")}}},
		{name: "state collision", text: "plugins: [{type: cache, args: {dump_file: data.dump}}]", files: []File{{Path: "data.dump"}}},
		{name: "TLS and webui", text: "api: {http: {ssl: {cert: cert.pem, key: key.pem, client_ca: ca.pem}, webui: {root: web}}}\nplugins: []", files: []File{{Path: "cert.pem"}, {Path: "key.pem"}, {Path: "ca.pem"}, {Path: "web/index.html", Content: []byte("hello")}}},
		{name: "aliased merge references", text: "defaults: &d {files: [data.txt]}\nplugins: [{args: {<<: *d}}]", files: []File{{Path: "data.txt", Content: []byte("example.com")}}},
		{name: "invalid path", text: "plugins: []", files: []File{{Path: "../outside"}}},
		{name: "conflicting path", text: "plugins: []", files: []File{{Path: "same.txt", Content: []byte("a")}, {Path: "same.txt", Content: []byte("b")}}},
		{name: "same content deduplicates", text: "plugins: []", files: []File{{Path: "same.txt", Content: []byte("a")}, {Path: "same.txt", Content: []byte("a")}}},
		{name: "configuration cannot collide", text: "plugins: []", files: []File{{Path: "config.yaml", Content: []byte("plugins: []")}}},
		{name: "non object", text: "[]"},
		{name: "missing download", text: "plugins: []", downloads: []mosdns.Download{{URL: upstream.URL + "/down", Name: "Down", LocalPath: "down.txt"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := archiveInput{Config: raw, YAML: c.text, Downloads: append([]mosdns.Download{}, c.downloads...), Files: []archiveFile{}, BaseURL: "http://config.example"}
			for _, file := range c.files {
				input.Files = append(input.Files, archiveFile{file.Path, file.Content})
				if file.Content == nil {
					input.Files[len(input.Files)-1].Content = []byte{}
				}
			}
			encoded, _ := json.Marshal(input)
			// Only the allocated loopback endpoint differs between recordings.
			key := bytes.ReplaceAll(encoded, []byte(upstream.URL), []byte("http://127.0.0.1:PYREF_PORT"))
			pythonInput := bytes.ReplaceAll(encoded, []byte(upstream.URL), []byte(pythonUpstream.URL))
			var reference struct {
				Answer  archiveAnswer  `json:"answer"`
				Fetches map[string]int `json:"fetches"`
			}
			if err := json.Unmarshal(oracle.Call(t, pythonInput, key), &reference); err != nil {
				t.Fatal(err)
			}
			want := reference.Answer
			for path, count := range reference.Fetches {
				referenceFetches[path] += count
			}

			before, _ := model.Marshal(configs)
			output, err := BuildTypedZIP(ctx, configs, c.text, c.downloads, c.files, "http://config.example")
			after, _ := model.Marshal(configs)
			if !bytes.Equal(before, after) {
				t.Fatal("profile input changed")
			}
			if want.Error != "" {
				if err == nil {
					t.Fatalf("Python rejected: %s", want.Error)
				}
				var safe *mosdns.ArchiveError
				if !errors.As(err, &safe) || safe.Message != want.Error {
					t.Fatalf("Go: %v; Python %s: %s", err, want.Class, want.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.NewReader(bytes.NewReader(output), int64(len(output)))
			if err != nil {
				t.Fatal(err)
			}
			got := archiveAnswer{Files: map[string]string{}, Names: []string{}, Modes: map[string]uint32{}}
			for _, entry := range archive.File {
				reader, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				got.Files[entry.Name] = base64.StdEncoding.EncodeToString(data)
				got.Names = append(got.Names, entry.Name)
				got.Modes[entry.Name] = uint32(entry.Mode().Perm())
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Go: %#v\nPython: %#v", got, want)
			}
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["/raw"] != 1 || referenceFetches["/raw"] != 1 {
		t.Fatalf("one fetch per source per engine: Go %v Python reference %v", calls, referenceFetches)
	}
	if !reflect.DeepEqual(calls, referenceFetches) {
		t.Fatalf("fetch side effects Go %v Python reference %v", calls, referenceFetches)
	}
}

func TestC29ArchiveRejectsUnsafeYAML(t *testing.T) {
	empty := &model.Config{Profile: &model.Profile{}}
	for _, text := range []string{"plugins: []\nplugins: []", "value: &a [*a]", "plugins: !!str []"} {
		_, err := BuildTypedZIP(context.Background(), empty, text, nil, nil, "")
		if !errors.Is(err, yamlx.ErrPolicy) && !errors.Is(err, yamlx.ErrAliasRecursion) {
			t.Fatalf("%q: %v", text, err)
		}
	}
}
