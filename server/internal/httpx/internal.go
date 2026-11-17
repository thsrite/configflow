package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"sync"

	"configflow/internal/jsonresp"
)

// InternalCallHeader carries the per-process token of in-process calls
// (backend/common/internal_call.py).
const InternalCallHeader = "X-ConfigFlow-Internal"

var (
	internalOnce  sync.Once
	internalToken string
)

// InternalCallToken is INTERNAL_CALL_TOKEN: random per process
// (secrets.token_urlsafe(32)), never stored or sent anywhere.
func InternalCallToken() string {
	internalOnce.Do(func() {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		internalToken = base64.RawURLEncoding.EncodeToString(b)
	})
	return internalToken
}

// InvokeRequest is one in-process REST call (mcp_server/invoker.call_api).
type InvokeRequest struct {
	Method string
	// Path is the full path ("/api/subscriptions").
	Path string
	// Query parameters in order.
	Query [][2]string
	// JSON is the request body; nil sends none.
	JSON any
	// HasJSON sends JSON even when it is nil (a JSON null body).
	HasJSON bool
	// Header adds request headers (e.g. X-ConfigFlow-Profile).
	Header http.Header
}

// InvokeResponse is the recorded response.
type InvokeResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// Invoke runs a request through the App as the MCP layer does with Flask's
// test client: remote address 127.0.0.1, Host "localhost", the internal call
// header set (so the authentication gate and require_auth let it through),
// the JSON body encoded like Flask's test client (sorted keys).
func (a *App) Invoke(ctx context.Context, in InvokeRequest) (*InvokeResponse, error) {
	raw := ""
	for i, pair := range in.Query {
		if i > 0 {
			raw += "&"
		}
		raw += url.QueryEscape(pair[0]) + "=" + url.QueryEscape(pair[1])
	}
	target := "http://localhost" + (&url.URL{Path: in.Path}).EscapedPath()
	if raw != "" {
		target += "?" + raw
	}
	var body io.Reader = http.NoBody
	if in.JSON != nil || in.HasJSON {
		data, err := jsonresp.Encode(in.JSON)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, in.Method, target, body)
	if err != nil {
		return nil, err
	}
	for key, values := range in.Header {
		req.Header[http.CanonicalHeaderKey(key)] = values
	}
	if in.JSON != nil || in.HasJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(InternalCallHeader, InternalCallToken())
	req.RemoteAddr = "127.0.0.1:0"
	req.Host = "localhost"
	recorder := &recorder{header: http.Header{}, status: 200}
	a.ServeHTTP(recorder, req)
	return &InvokeResponse{Status: recorder.status, Header: recorder.header, Body: recorder.body.Bytes()}, nil
}

type recorder struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
}
func (r *recorder) Write(p []byte) (int, error) {
	r.WriteHeader(200)
	return r.body.Write(p)
}
