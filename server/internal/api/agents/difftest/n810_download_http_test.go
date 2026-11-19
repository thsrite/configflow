package difftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

func n810DownloadVariant(r downloadRequest, python response) (downloadRequest, bool) {
	switch r.name {
	case "review if-modified-since overflow", "review if-modified-since c long overflow", "review if-modified-since offset overflow", "review if-range overflow", "review if-range date without weekday":
	default:
		return r, false
	}
	if python.status != 500 {
		return r, false
	}
	parsed, err := jsonx.Parse(python.body)
	if err != nil {
		return r, false
	}
	body, ok := parsed.(*jsonx.Object)
	if !ok {
		return r, false
	}
	text := body.GetString("message")
	if !(strings.HasPrefix(text, "signed integer") || strings.HasPrefix(text, "Python int too large") || strings.HasPrefix(text, "days=") || strings.HasPrefix(text, "416 Requested Range")) {
		return r, false
	}
	next := r
	next.headers = map[string]string{}
	for key, value := range r.headers {
		if key != "If-Modified-Since" && key != "If-Range" && key != "Range" {
			next.headers[key] = value
		}
	}
	return next, true
}
func n810DownloadHTTP(t *testing.T, r downloadRequest, python, native, reference response) []string {
	t.Helper()
	original := map[string]any{"name": r.name, "method": r.method, "path": r.path, "headers": r.headers, "known_httpx": r.httpx, "python_status": python.status, "python_body": string(python.body)}
	row := n810APIRow{Original: original, SHA: fmt.Sprintf("%x", sha256.Sum256(n810Canonical(original))), Kind: "download-date"}
	path := "testdata/n810_download_http_python.jsonl"
	if os.Getenv("N810_RECORD_API_PYTHON") != "" {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(row); err != nil {
			t.Fatal(err)
		}
		file.Close()
	} else {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			var rule n810APIRow
			if err := json.Unmarshal(line, &rule); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(rule.Original))) != rule.SHA {
				t.Fatal("original download row changed")
			}
			if rule.Original["name"] != r.name {
				continue
			}
			if !bytes.Equal(n810Canonical(original), n810Canonical(rule.Original)) {
				t.Fatal("bound download input/result changed", r.name)
			}
			found = true
		}
		if !found {
			t.Fatal("unbound download condition", r.name)
		}
	}
	diffs := compareDownload(reference, native)
	if etagFloatOnly(reference, native) {
		var filtered []string
		for _, diff := range diffs {
			if !strings.HasPrefix(diff, "header ETag ") {
				filtered = append(filtered, diff)
			}
		}
		diffs = filtered
	}
	return diffs
}

// nativeFileDiagnostic compares only the native spelling of two established I/O
// failures. Every status/header and non-message body field is still compared.
func nativeFileDiagnostic(python, native response) (any, any, bool) {
	if python.status != 500 || native.status != 500 {
		return nil, nil, false
	}
	p, err := jsonx.Parse(python.body)
	if err != nil {
		return nil, nil, false
	}
	g, err := jsonx.Parse(native.body)
	if err != nil {
		return nil, nil, false
	}
	a, aok := p.(*jsonx.Object)
	b, bok := g.(*jsonx.Object)
	if !aok || !bok || a.Len() != 2 || b.Len() != 2 || a.Value("success") != false || b.Value("success") != false {
		return nil, nil, false
	}
	old, text := a.GetString("message"), b.GetString("message")
	category := ""
	if strings.HasPrefix(old, "[Errno 2] No such file or directory: ") && strings.HasSuffix(text, ": no such file or directory") {
		category = "not found"
	}
	if strings.HasPrefix(old, "[Errno 21] Is a directory: ") && strings.HasSuffix(text, ": is a directory") {
		category = "directory"
	}
	if strings.HasPrefix(old, "[Errno 13] Permission denied: ") && strings.HasSuffix(text, ": permission denied") {
		category = "permission denied"
	}
	if category == "" {
		return nil, nil, false
	}
	a.Set("message", category)
	b.Set("message", category)
	return a, b, true
}
