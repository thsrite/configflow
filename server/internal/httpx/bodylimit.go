package httpx

import (
	"errors"
	"io"
	"net/http"
)

// LimitRequestBody applies the Edge's path-specific request body limits to
// the backend listener without changing its proxy headers or response encoding.
func LimitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		limit := int64(DefaultBodyLimit)
		if oxidnsUpload.MatchString(normalizeURI(req.URL.Path)) {
			limit = OxidnsBodyLimit
		}
		if limitRequestBody(w, req, limit) {
			next.ServeHTTP(w, req)
		}
	})
}

// limitRequestBody rejects known oversized bodies before dispatch and limits
// streaming bodies too. Both listeners use this implementation.
func limitRequestBody(w http.ResponseWriter, req *http.Request, limit int64) bool {
	if req.ContentLength > limit {
		nginxError(w, req, http.StatusRequestEntityTooLarge)
		return false
	}
	if req.Body != nil {
		req.Body = &limitedRequestBody{ReadCloser: http.MaxBytesReader(w, req.Body, limit)}
	}
	return true
}

// Keep overflow visible even when a form/stream consumer handles its read error.
type limitedRequestBody struct {
	io.ReadCloser
	exceeded bool
}

func (b *limitedRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		b.exceeded = true
	}
	return n, err
}

func bodyLimitExceeded(req *http.Request) bool {
	body, ok := req.Body.(*limitedRequestBody)
	return ok && body.exceeded
}
