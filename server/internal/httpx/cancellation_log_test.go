package httpx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
)

func TestRequestCancellationIsNotAnApplicationException(t *testing.T) {
	for _, tc := range []struct {
		name       string
		disconnect bool
		err        error
		wantLog    bool
	}{
		{"disconnected wrapped cancellation", true, context.Canceled, false},
		{"live request internal cancellation", false, context.Canceled, true},
		{"disconnected unrelated error", true, errors.New("synthetic application failure"), true},
		{"disconnected internal timeout", true, context.DeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root, err := logx.Setup(logx.Config{Level: logx.Error, Console: &output})
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			entered, finalized := make(chan struct{}), make(chan struct{})
			s := httpxtest.New(t, httpxtest.Options{
				Register: httpxtest.Routes(func(r *httpx.Router) {
					r.GET("/api/cancellation", "test.cancellation", func(c *httpx.Ctx) (*httpx.Response, error) {
						close(entered)
						if tc.disconnect {
							<-c.Request.Context().Done()
						}
						return nil, fmt.Errorf("finish synthetic request: %w", tc.err)
					})
				}),
				Setup: func(a *httpx.App) {
					a.AfterRequest(func(*httpx.Ctx, *httpx.Response) { close(finalized) })
				},
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL()+"/api/cancellation", nil)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusInternalServerError {
						err = fmt.Errorf("status %d", resp.StatusCode)
					}
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("request did not enter handler")
			}
			if tc.disconnect {
				cancel()
			}
			if err := <-done; tc.disconnect && !errors.Is(err, context.Canceled) || !tc.disconnect && err != nil {
				t.Fatalf("HTTP client result: %v", err)
			}
			select {
			case <-finalized:
			case <-time.After(10 * time.Second):
				t.Fatal("disconnected request did not finish")
			}
			logged := strings.Contains(output.String(), "Exception on /api/cancellation")
			if logged != tc.wantLog {
				t.Fatalf("application exception logged=%v, want %v: %s", logged, tc.wantLog, output.String())
			}
		})
	}
}
