package deploy

import (
	"bytes"
	"configflow/internal/jsonx"
	"configflow/internal/rulefetch"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPythonRulesAndLocations(t *testing.T) {
	data, err := os.ReadFile("testdata/rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Rows []struct {
			Text, Kind, Result, Error string
			Mixed                     bool
		}
		Locations []struct {
			Text   string
			Line   int
			Result string
		}
		Contexts     []struct{ Name, Path, Message, Result string }
		Source       json.RawMessage
		Materialized []struct{ URL, Result, Error string }
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for i, row := range corpus.Rows {
		got, err := ConvertMosDNSRules([]byte(row.Text), row.Kind, row.Mixed)
		if row.Error != "" {
			if err == nil {
				t.Fatalf("row %d accepted", i)
			}
			if !strings.Contains(row.Text, "broken") && err.Error() != row.Error {
				t.Errorf("row %d error\n%s\nwant\n%s", i, err, row.Error)
			}
		} else {
			want, _ := base64.StdEncoding.DecodeString(row.Result)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("row %d %q %v want %q", i, got, err, want)
			}
		}
	}
	for _, row := range corpus.Locations {
		if got := SourceExcerpt(row.Text, row.Line); got != row.Result {
			t.Errorf("excerpt %q line %d got %q want %q", row.Text, row.Line, got, row.Result)
		}
	}
	for _, row := range corpus.Contexts {
		if got := PreparationContext(preparation(row.Message), row.Name, row.Path, "").Error(); got != row.Result {
			t.Errorf("context %q want %q", got, row.Result)
		}
	}
	config, err := jsonx.Parse(corpus.Source)
	if err != nil {
		t.Fatal(err)
	}
	src := NewSource(config.(*jsonx.Object), nil)
	for _, row := range corpus.Materialized {
		got, err := MaterializeRule(context.Background(), row.URL, src, "", MaterializeOptions{})
		if row.Error != "" {
			if err == nil || err.Error() != row.Error {
				t.Errorf("callback %s got %q %v want %q", row.URL, got, err, row.Error)
			}
		} else {
			want, _ := base64.StdEncoding.DecodeString(row.Result)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("callback %s %q %v", row.URL, got, err)
			}
		}
	}
}

func TestLoopbackRuleDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("DOMAIN,example.com"))
	}))
	defer srv.Close()
	src := NewSource(jsonx.NewObject(), nil)
	for _, tc := range []struct {
		path   string
		failed bool
	}{{"/good", false}, {"/bad", true}} {
		data, err := MaterializeRule(context.Background(), srv.URL+tc.path, src, "", MaterializeOptions{})
		if tc.failed {
			if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
				t.Fatal(data, err)
			}
		} else if err != nil || string(data) != "DOMAIN,example.com" {
			t.Fatal(data, err)
		}
	}
}

type timeoutTransport struct {
	t     *testing.T
	calls int
}

func (f *timeoutTransport) Do(ctx context.Context, r rulefetch.TransportRequest) (*rulefetch.Response, error) {
	f.calls++
	if _, ok := ctx.Deadline(); ok {
		f.t.Error("download acquired an extra overall deadline")
	}
	if r.ConnectTimeout != 10*time.Second || r.ReadTimeout != 60*time.Second {
		f.t.Error("download phase timeouts changed", r.ConnectTimeout, r.ReadTimeout)
	}
	return &rulefetch.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("DOMAIN,example.com")), URL: r.URL}, nil
}
func TestDownloadKeepsPerAttemptTimeouts(t *testing.T) {
	previous := rulefetch.DefaultClient
	transport := &timeoutTransport{t: t}
	rulefetch.DefaultClient = &rulefetch.Client{Transport: transport}
	t.Cleanup(func() { rulefetch.DefaultClient = previous })
	data, err := fetchBytes(context.Background(), "https://rules.example/source", NewSource(jsonx.NewObject(), nil))
	if err != nil || string(data) != "DOMAIN,example.com" || transport.calls != 1 {
		t.Fatal(string(data), err, transport.calls)
	}
}
