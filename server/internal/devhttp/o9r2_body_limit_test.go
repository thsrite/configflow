package devhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// O9 round 2: a device reply above MaxResponseBytes is refused, not buffered.
func TestO9R2ReplyLimit(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{"exact limit is read", MaxResponseBytes, false},
		{"one byte over", MaxResponseBytes + 1, true},
		{"endless body", 4 * MaxResponseBytes, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.Copy(w, io.LimitReader(zeros{}, tc.size))
			}))
			defer srv.Close()
			resp, err := New(Config{}).Do(context.Background(), "GET", srv.URL+"/rest/ip/address", nil, 10*time.Second)
			if tc.wantErr {
				if !errors.Is(err, ErrResponseTooLarge) || ClassOf(err) != ConnectionError {
					t.Fatalf("err = %v, want ConnectionError(ErrResponseTooLarge)", err)
				}
				return
			}
			if err != nil || int64(len(resp.Body)) != tc.size {
				t.Fatalf("err = %v, body %d bytes, want %d", err, len(resp.Body), tc.size)
			}
		})
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
