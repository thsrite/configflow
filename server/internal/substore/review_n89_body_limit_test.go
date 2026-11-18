package substore

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A streaming source keeps the adversarial fixture itself small.
type reviewBodySource struct{}

func (reviewBodySource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestReviewN89ResponseBodyLimit(t *testing.T) {
	const limit = 64 << 20
	for _, encoding := range []string{"identity", "gzip", "deflate", "raw-deflate", "gzip, deflate"} {
		t.Run(encoding, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if encoding == "identity" {
					_, _ = io.CopyN(w, reviewBodySource{}, limit+1)
					return
				}
				wireEncoding := encoding
				if encoding == "raw-deflate" {
					wireEncoding = "deflate"
				}
				w.Header().Set("Content-Encoding", wireEncoding)
				var writer io.WriteCloser
				switch encoding {
				case "gzip":
					writer = gzip.NewWriter(w)
				case "deflate":
					writer = zlib.NewWriter(w)
				case "raw-deflate":
					writer, _ = flate.NewWriter(w, flate.DefaultCompression)
				default:
					outer := zlib.NewWriter(w)
					defer outer.Close()
					writer = gzip.NewWriter(outer)
				}
				_, _ = io.CopyN(writer, reviewBodySource{}, limit+1)
				_ = writer.Close()
			}))
			defer server.Close()
			response, err := (RequestsClient{}).Do(context.Background(), &Request{Method: "GET", URL: server.URL, Timeout: Seconds(10)})
			if err == nil || response != nil {
				length := 0
				if response != nil {
					length = len(response.Body)
				}
				t.Fatalf("accepted body above 64 MiB: bytes=%d err=%v", length, err)
			}
			var requestError *RequestError
			if !errors.As(err, &requestError) || requestError.Class != "ContentDecodingError" || !errors.Is(err, ErrResponseTooLarge) || err.Error() != msgResponseTooLarge {
				t.Fatalf("oversize error: %v", err)
			}
		})
	}
}

func TestReviewN89ResponseBodyAtLimit(t *testing.T) {
	const limit = 64 << 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = io.CopyN(writer, reviewBodySource{}, limit)
		_ = writer.Close()
	}))
	defer server.Close()
	response, err := (RequestsClient{}).Do(context.Background(), &Request{Method: "GET", URL: server.URL, Timeout: Seconds(10)})
	if err != nil || response == nil || len(response.Body) != limit || !bytes.Equal(response.Body[:16], bytes.Repeat([]byte{'x'}, 16)) {
		t.Fatalf("body at limit failed: err=%v", err)
	}
}

// The inner gzip has a huge comment and an empty payload: the final body is
// tiny, but decoding its outer wrapper must still enforce the intermediate cap.
func TestReviewN89ResponseIntermediateLimit(t *testing.T) {
	var empty bytes.Buffer
	writer := gzip.NewWriter(&empty)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	inner := empty.Bytes()
	inner[3] = 16 // FCOMMENT
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip, gzip")
		outer := gzip.NewWriter(w)
		_, _ = outer.Write(inner[:10])
		_, _ = io.CopyN(outer, reviewBodySource{}, 64<<20)
		_, _ = outer.Write([]byte{0})
		_, _ = outer.Write(inner[10:])
		_ = outer.Close()
	}))
	defer server.Close()
	response, err := (RequestsClient{}).Do(context.Background(), &Request{Method: "GET", URL: server.URL, Timeout: Seconds(10)})
	if response != nil || !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("intermediate layer limit bypassed: response=%v err=%v", response != nil, err)
	}
}
