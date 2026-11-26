package rulefetch

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// A valid tiny inner gzip member followed by huge padding produces a small
// final body: bounding only the final response misses the intermediate bomb.
func TestRuleDownloadBoundsIntermediateDecodedLayers(t *testing.T) {
	var inner bytes.Buffer
	member := gzip.NewWriter(&inner)
	if _, err := member.Write([]byte("tiny")); err != nil {
		t.Fatal(err)
	}
	if err := member.Close(); err != nil {
		t.Fatal(err)
	}
	for _, coding := range []string{"gzip", "deflate"} {
		t.Run(coding, func(t *testing.T) {
			var packed bytes.Buffer
			var outer io.WriteCloser
			if coding == "gzip" {
				outer = gzip.NewWriter(&packed)
			} else {
				outer = zlib.NewWriter(&packed)
			}
			if _, err := outer.Write(inner.Bytes()); err != nil {
				t.Fatal(err)
			}
			padding := make([]byte, 1<<20)
			for i := int64(0); i <= MaxDecodedBytes; i += int64(len(padding)) {
				if _, err := outer.Write(padding); err != nil {
					t.Fatal(err)
				}
			}
			if err := outer.Close(); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", "gzip, "+coding)
				_, _ = w.Write(packed.Bytes())
			}))
			defer server.Close()
			response, err := DefaultClient.Download(t.Context(), server.URL, DownloadRequest{Config: &SourceConfig{}})
			if response != nil {
				response.Close()
			}
			if !errors.Is(err, ErrSizeLimit) {
				t.Fatalf("intermediate decoded bomb was not refused: %v", err)
			}
		})
	}
}

// Promoted from TestO9ReproRuleDownloadHasNoDecodedSizeLimit. The compressed
// input is small, but its decoded size exceeds the actual production bound.
func TestRuleDownloadEnforcesDecodedSizeLimit(t *testing.T) {
	const inflated = 192 << 20
	var packed bytes.Buffer
	gz, err := gzip.NewWriterLevel(&packed, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for i := 0; i < inflated/len(chunk); i++ {
		if _, err := gz.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(packed.Bytes())
	}))
	defer source.Close()
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "eager", true: "stream"}[stream], func(t *testing.T) {
			response, err := DefaultClient.Download(context.Background(), source.URL+"/rules.list", DownloadRequest{Config: &SourceConfig{}, Stream: stream})
			if response != nil {
				defer response.Close()
				if stream {
					_, err = io.Copy(io.Discard, response.Body)
				}
			}
			if !errors.Is(err, ErrSizeLimit) {
				t.Fatalf("decoded bomb was not refused by size: %v", err)
			}
		})
	}
	// A cache refresh uses the same decoded boundary and cannot replace the last
	// validated bytes with either a partial response or the compressed bomb.
	ctx, r := cacheRepo(t, nil)
	rule := remoteSource()
	rule.URL = strp(source.URL + "/rules.list")
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("example.com\n"), Text: true}, nil); err != nil {
		t.Fatal(err)
	}
	path := cachePath(t, r, rule)
	content, _, err := RefreshRule(ctx, r, rule, &SourceConfig{}, CacheOptions{})
	if !errors.Is(err, ErrSizeLimit) || content != nil {
		t.Fatalf("cache accepted decoded bomb: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "example.com\n" {
		t.Fatalf("failed refresh replaced cache: %q %v", data, err)
	}
}

type closeCountingBody struct {
	io.Reader
	closed int
}

func (b *closeCountingBody) Close() error { b.closed++; return nil }

func TestDecodedLimitExactBoundaryAndClose(t *testing.T) {
	for _, length := range []int{3, 4, 5} {
		source := &closeCountingBody{Reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, length))}
		body := &decodedLimitBody{source: source, remaining: 4}
		data, err := io.ReadAll(body)
		if length <= 4 && (err != nil || len(data) != length) || length > 4 && (!errors.Is(err, ErrSizeLimit) || len(data) != 4) {
			t.Fatalf("length %d: %q %v", length, data, err)
		}
		_ = body.Close()
		_ = body.Close()
		if source.closed != 1 {
			t.Fatalf("closed %d times", source.closed)
		}
	}
}
