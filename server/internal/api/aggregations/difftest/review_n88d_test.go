package difftest

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestReviewN88dProviderByteSamples(t *testing.T) {
	w := newWorld(t, seed(t, true, ""), map[string]string{"s": feed})
	// Captured from the pre-S-pass Go tree 1fd89e1c1, independently of the
	// current typed implementation. Compare raw bodies, including YAML order.
	expected := map[string]string{
		"":      "47fac98b72d3c5aed9f983ec1b0ed65bb72e5af3797b0d92cc44ac3e07c64619",
		"surge": "1d48f377ae6e976013abfc16ec71369869b20af84729b1cfdccc508c910c407c",
		"loon":  "d8001c28a678cd435000baf699b43470c6dcdf31a4508d71dfa3b2b101683691",
	}
	for _, format := range []string{"", "surge", "loon"} {
		t.Run(map[string]string{"": "mihomo", "surge": "surge", "loon": "loon"}[format], func(t *testing.T) {
			path := "/api/aggregations/a/provider"
			if format != "" {
				path += "?format=" + format
			}
			// The existing Flask comparison checks status, semantic output,
			// Chinese diagnostics and persisted effects for this same request.
			w.call("GET", path, "", true)
			got := w.srv.Get(path, httpxtest.Header("Authorization", w.token))
			if got.Status != 200 {
				t.Fatalf("status=%d body=%s", got.Status, got.Body)
			}
			if digest := fmt.Sprintf("%x", sha256.Sum256(got.Body)); digest != expected[format] {
				t.Fatalf("provider bytes changed: digest=%s body=%q", digest, got.Body)
			}
		})
	}
}
