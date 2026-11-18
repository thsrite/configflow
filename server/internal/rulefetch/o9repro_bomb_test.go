//go:build o9repro

package rulefetch

// O9 round 1 reproduction (fails until fixed; excluded from the gate by the
// o9repro build tag): go test -tags o9repro -run TestO9Repro ./internal/rulefetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A remote rule source answers ~200 KB of gzip that inflates to 192 MiB.
// RefreshRemoteRule's download (RequestRule + Content, no MaxBytes) reads it
// all into memory; a larger bomb exhausts the panel's memory.
func TestO9ReproRuleDownloadHasNoDecodedSizeLimit(t *testing.T) {
	const inflated = 192 << 20
	var packed bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	chunk := make([]byte, 1<<20)
	for i := 0; i < inflated/len(chunk); i++ {
		gz.Write(chunk)
	}
	gz.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(packed.Bytes())
	}))
	defer source.Close()
	response, err := DefaultClient.RequestRule(context.Background(), source.URL+"/rules.list",
		Request{Config: sysConfig(), ConnectTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second})
	if err != nil {
		return
	}
	data, err := response.Content()
	if err == nil && len(data) >= inflated {
		t.Fatalf("%d compressed bytes inflated to %d bytes in memory without a limit", packed.Len(), len(data))
	}
}
