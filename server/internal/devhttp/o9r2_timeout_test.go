package devhttp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// O9 round 2 L1: slow-drip device replies that reset the per-read watchdog
// still fail once totalTimeout (timeout * TotalTimeoutMultiplier) elapses,
// returning ReadTimeout(ErrTotalTimeout), while slower multi-chunk replies
// within budget continue to succeed.
func TestO9R2TotalTimeout(t *testing.T) {
	const (
		readTimeout = 60 * time.Millisecond // total cap = 6 * 60ms = 360ms
		dripStep    = 30 * time.Millisecond // half of readTimeout so per-read never fires
	)
	tests := []struct {
		name           string
		handler        func(net.Conn)
		wantErr        bool
		wantTotalCause bool
		wantBytes      string
	}{
		{
			name: "slow-drip Content-Length body exceeds total cap",
			handler: func(conn net.Conn) {
				defer conn.Close()
				readRequest(conn)
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 40\r\n\r\n"))
				for range 40 {
					time.Sleep(dripStep)
					if _, err := conn.Write([]byte("a")); err != nil {
						return
					}
				}
			},
			wantErr:        true,
			wantTotalCause: true,
		},
		{
			name: "slow-drip chunked body exceeds total cap",
			handler: func(conn net.Conn) {
				defer conn.Close()
				readRequest(conn)
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"))
				for range 40 {
					time.Sleep(dripStep)
					if _, err := conn.Write([]byte("1\r\na\r\n")); err != nil {
						return
					}
				}
				_, _ = conn.Write([]byte("0\r\n\r\n"))
			},
			wantErr:        true,
			wantTotalCause: true,
		},
		{
			name: "slow-drip headers fail with ReadTimeout",
			handler: func(conn net.Conn) {
				defer conn.Close()
				readRequest(conn)
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n"))
				for range 30 {
					time.Sleep(dripStep)
					if _, err := conn.Write([]byte("X-Slow: 1\r\n")); err != nil {
						return
					}
				}
				_, _ = conn.Write([]byte("Content-Length: 2\r\n\r\n{}"))
			},
			wantErr: true,
		},
		{
			name: "negative control: multi-chunk within total cap succeeds",
			handler: func(conn net.Conn) {
				defer conn.Close()
				readRequest(conn)
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\n"))
				for _, b := range []byte("[1 ]") {
					time.Sleep(dripStep)
					_, _ = conn.Write([]byte{b})
				}
			},
			wantErr:   false,
			wantBytes: "[1 ]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr := serve(t, tc.handler)
			c := New(Config{})
			defer c.Close()

			resp, err := c.Do(context.Background(), "GET", "http://"+addr+"/rest/ip/route", nil, readTimeout)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got status %d body %q, want ReadTimeout", resp.Status, resp.Body)
				}
				var de *Error
				if !errors.As(err, &de) || de.Class != ReadTimeout || !de.Timeout() {
					t.Fatalf("err = %v, want *Error{Class: ReadTimeout}", err)
				}
				if tc.wantTotalCause && !errors.Is(err, ErrTotalTimeout) {
					t.Fatalf("err = %v, want ErrTotalTimeout", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(resp.Body) != tc.wantBytes {
				t.Fatalf("body = %q, want %q", resp.Body, tc.wantBytes)
			}
		})
	}
}
