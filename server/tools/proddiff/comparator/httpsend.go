package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// contentSyncHeader marks a content-sync observer timeout in configflow_proddiff builds.
const contentSyncHeader = "x-configflow-proddiff-error"

// contentSyncSeconds is the GET budget of the Agent content-sync routes: the
// server waits up to 120 s for the real Checker, the client 30 s more.
const contentSyncSeconds = 150

var contentSyncRoutes = agentComparisonRoutes

// errObserverTimeout is a harness failure; nothing is classified.
var errObserverTimeout = errors.New("content sync observer timed out; no differences were classified")

// transportError describes one failed request for the private trace (no texts).
type transportError struct {
	Type  string
	Errno *int
}

// idleConn applies the timeout to every blocking read/write, like a socket timeout.
type idleConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

// sender performs one request; tests replace it.
type sender func(port int, method, path, token string, body *Map, timeout time.Duration) (reply, *transportError, error)

// send issues one request to 127.0.0.1:port. Transport failures (no HTTP
// response) return status 0 and a transportError; HTTP error statuses are
// replies. A body read failure after the response started is a framework error.
func send(port int, method, path, token string, body *Map, timeout time.Duration) (reply, *transportError, error) {
	var payload io.Reader
	if body != nil {
		payload = strings.NewReader(dumpJSON(body, dumpOptions{indent: -1, ensureASCII: true}))
	}
	request, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), payload)
	if err != nil {
		return reply{}, nil, fmt.Errorf("build request: %w", err)
	}
	request.Host = "127.0.0.1:5001"
	request.Header.Set("X-Forwarded-Host", "127.0.0.1:5001")
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	dialer := &net.Dialer{Timeout: timeout}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:              nil,
			DisableKeepAlives:  true,
			DisableCompression: true,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				return &idleConn{Conn: conn, timeout: timeout}, nil
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return transportFailure, classifyTransport(err), nil
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return reply{}, nil, fmt.Errorf("read response body: %w", err)
	}
	headers := map[string]string{}
	for name, values := range response.Header {
		headers[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	return httpReply(response.StatusCode, response.Header.Get("Content-Type"), data, headers), nil, nil
}

func classifyTransport(err error) *transportError {
	t := &transportError{Type: "URLError"}
	var netErr net.Error
	var errno syscall.Errno
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		t.Type = "TimeoutError"
	case errors.Is(err, syscall.ECONNREFUSED):
		t.Type = "ConnectionRefusedError"
	case errors.Is(err, syscall.ECONNRESET):
		t.Type = "ConnectionResetError"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		t.Type = "RemoteDisconnected"
	}
	if errors.As(err, &errno) {
		n := int(errno)
		t.Errno = &n
	}
	return t
}

// checkedReply fails the run when the content-sync observer timed out.
func checkedReply(r reply) (reply, error) {
	if _, ok := r.Headers[contentSyncHeader]; ok {
		return r, errObserverTimeout
	}
	return r, nil
}

// asker sends one request to one side with a socket timeout in seconds.
type asker func(timeout int) (reply, error)

// diffFunc compares two replies of the case under comparison.
type diffFunc func(left, right reply) ([]diff, error)

// httpResult is the outcome of comparing one case.
type httpResult struct {
	left, right reply
	rescued     int    // bracket round that produced agreement, 0 if none
	retries     [2]int // per-side transport retries
}

// compareHTTP recovers only one-sided transport failures, then compares
// adjacent replies. Each side has three transport retries per case
// (40/80/160 s). Windowed GETs may additionally alternate sides for at most
// three bracket rounds; a transport failure or differing HTTP status stops
// bracketing immediately.
func compareHTTP(left, right asker, diffs diffFunc, bracket bool) (httpResult, error) {
	const attempts = 3
	senders := [2]asker{left, right}
	var replies [2]reply
	var retries [2]int
	for i := range senders {
		r, err := senders[i](20)
		if err != nil {
			return httpResult{}, err
		}
		if r, err = checkedReply(r); err != nil {
			return httpResult{}, err
		}
		replies[i] = r
	}
	last := 1
	recover := func(index int) error {
		for replies[index].Status == 0 && retries[index] < 3 {
			retries[index]++
			r, err := senders[index](20 << retries[index])
			if err != nil {
				return err
			}
			if r, err = checkedReply(r); err != nil {
				return err
			}
			replies[index] = r
		}
		return nil
	}
	if (replies[0].Status == 0) != (replies[1].Status == 0) {
		last = 1
		if replies[0].Status == 0 {
			last = 0
		}
		if err := recover(last); err != nil {
			return httpResult{}, err
		}
	}
	cannotBracket := func() bool {
		return replies[0].Status == 0 || replies[1].Status == 0 || replies[0].Status != replies[1].Status
	}
	done := func(rescued int) (httpResult, error) {
		return httpResult{left: replies[0], right: replies[1], rescued: rescued, retries: retries}, nil
	}
	if cannotBracket() || !bracket {
		return done(0)
	}
	found, err := diffs(replies[0], replies[1])
	if err != nil {
		return httpResult{}, err
	}
	if len(found) == 0 {
		return done(0)
	}
	for step := 0; step < attempts*2; step++ {
		// Recovery may have made the left side the latest reply. Always ask the
		// other side next, so equality never uses replies separated by another one.
		last = 1 - last
		r, err := senders[last](20)
		if err != nil {
			return httpResult{}, err
		}
		if r, err = checkedReply(r); err != nil {
			return httpResult{}, err
		}
		replies[last] = r
		if err := recover(last); err != nil {
			return httpResult{}, err
		}
		if cannotBracket() {
			return done(0)
		}
		found, err := diffs(replies[0], replies[1])
		if err != nil {
			return httpResult{}, err
		}
		if len(found) == 0 {
			return done(1 + step/2)
		}
	}
	return done(0)
}

// scrub replaces the server's own temporary data path, which is not a difference.
func scrub(r reply, dataDir string) reply {
	marker := []byte(dataDir)
	out := r
	out.Body = bytes.ReplaceAll(r.Body, marker, []byte("<DATA_DIR>"))
	out.Headers = map[string]string{}
	for name, value := range r.Headers {
		out.Headers[name] = strings.ReplaceAll(value, dataDir, "<DATA_DIR>")
	}
	return out
}
