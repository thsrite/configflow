package mosdns

import (
	"errors"
	"fmt"
	"testing"

	"configflow/internal/rulefetch"
)

func TestTypedDownloadFailureContracts(t *testing.T) {
	transport := &rulefetch.FetchError{Class: "HTTPError", Status: 503, Message: "synthetic secret"}
	for _, tt := range []struct {
		name    string
		err     error
		bad     bool
		details string
	}{
		{"invalid URL", rulefetch.ErrInvalidURL, true, "exception_type=ValueError"},
		{"internal redirect", rulefetch.ErrInternalRedirect, true, "exception_type=ValueError"},
		{"size", rulefetch.ErrSizeLimit, true, "exception_type=ValueError"},
		{"encoding", errNotUTF8, true, "exception_type=UnicodeDecodeError"},
		{"HTTP", transport, false, "exception_type=HTTPError status=503"},
		{"download", &rulefetch.DownloadError{Err: transport, Kind: "HTTPError"}, false, "exception_type=RequestException status=503"},
		{"aggregate", &rulefetch.RuleFetchError{LastError: transport}, false, "exception_type=RuleFetchError"},
		{"plain validation", errors.New("synthetic validation"), true, "exception_type=ValueError"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, err := range []error{tt.err, fmt.Errorf("download: %w", tt.err)} {
				if isBadRequest(err) != tt.bad || safeDetails(err) != tt.details {
					t.Fatalf("typed failure: bad=%v details=%q; want %v %q", isBadRequest(err), safeDetails(err), tt.bad, tt.details)
				}
			}
		})
	}
}
