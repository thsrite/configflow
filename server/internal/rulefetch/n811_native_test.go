package rulefetch

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestN811HTMLAndCharset(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"entities", `<meta content="A &amp; B &#169;">`, "A & B ©"},
		{"comment", `<!-- <meta content="hidden"> --><meta content="visible">`, "visible"},
		{"script", `<script>"<meta content='hidden'>"</script><meta content="visible">`, "visible"},
		{"style", `<style>.x {content:"<meta content='hidden'>"}</style><meta content="visible">`, "visible"},
		{"last-meta", `<meta content="first"><META content="last">`, "last"},
		{"duplicate-attribute", `<meta content="first" content="last">`, "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := metaContent(tc.input); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	response := &Response{Header: http.Header{"Content-Type": {`text/html; charset="ISO-8859-1"`}}}
	if got := response.Charset(); got != "iso-8859-1" {
		t.Fatalf("charset %q", got)
	}
	if got := latin1Text(string([]byte{'c', 'a', 'f', 0xe9})); got != "café" {
		t.Fatalf("Latin-1 header %q", got)
	}
	// Negative controls: encoded entities and Latin-1 bytes must reach decoding.
	if metaContent(`<meta content="A &amp; B">`) == "A &amp; B" || response.Charset() == "" {
		t.Fatal("native decoding path was bypassed")
	}
	for _, value := range []string{"&#1;", "&#x;", "&#x10ffff;"} {
		if got := pyUnescape(value); got != html.UnescapeString(value) {
			t.Fatalf("entity %q: %q", value, got)
		}
	}
}

func TestN811ErrorLabelsAndRawRequest(t *testing.T) {
	failure := &RuleFetchError{message: "synthetic failure"}
	if failure.ErrorClass() != "RuleFetchError" || (&FetchError{Class: "ReadTimeout"}).ErrorClass() != "ReadTimeout" {
		t.Fatal("diagnostic labels changed")
	}
	header, err := basicAuthHeader("café", "secret")
	if err != nil || header != "Basic Y2Fm6TpzZWNyZXQ=" {
		t.Fatalf("Basic header %q %v", header, err)
	}
	if _, err := basicAuthHeader("用户", "secret"); err == nil {
		t.Fatal("unencodable credentials accepted")
	}
	client := &Client{Transport: n811Transport(func(ctx context.Context, req TransportRequest) (*Response, error) {
		if req.Method != "HEAD" || req.URL != "https://example.com/rules" {
			t.Fatalf("raw request %+v", req)
		}
		return &Response{StatusCode: 200, URL: req.URL, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	response, err := client.Download(context.Background(), "https://example.com/rules", DownloadRequest{Method: "head", Config: &SourceConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	response.Close()
	if !errors.Is((&FetchError{Err: context.Canceled}), context.Canceled) {
		t.Fatal("lost error chain")
	}
}

type n811Transport func(context.Context, TransportRequest) (*Response, error)

func (f n811Transport) Do(ctx context.Context, req TransportRequest) (*Response, error) {
	return f(ctx, req)
}
