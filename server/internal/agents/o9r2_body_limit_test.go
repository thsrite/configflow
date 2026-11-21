package agents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// O9 round 2: replies of requests without their own MaxBody are capped.
func TestO9R2DefaultReplyLimit(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		chunked bool
		wantErr bool
	}{
		{"exact limit is read", DefaultMaxBody, false, false},
		{"one byte over (Content-Length)", DefaultMaxBody + 1, false, true},
		{"one byte over (chunked)", DefaultMaxBody + 1, true, true},
		{"endless body", 4 * DefaultMaxBody, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !tc.chunked {
					w.Header().Set("Content-Length", strconv.FormatInt(tc.size, 10))
				}
				_, _ = io.Copy(w, io.LimitReader(zeroReader{}, tc.size))
			}))
			defer srv.Close()
			response, err := HTTPClient{}.Do(context.Background(), Request{Method: http.MethodGet, URL: srv.URL + "/api/status", Timeout: Pair(3, 12), NoProxy: true})
			if tc.wantErr {
				if !errors.Is(err, ErrResponseTooLarge) || !IsTransportError(err) {
					t.Fatalf("err = %v, want TransportError(ErrResponseTooLarge)", err)
				}
				if !strings.Contains(err.Error(), "64 MiB") {
					t.Fatalf("message %q does not name the limit", err.Error())
				}
				return
			}
			if err != nil || int64(len(response.Body)) != tc.size {
				t.Fatalf("err = %v, body %d bytes, want %d", err, len(response.Body), tc.size)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
