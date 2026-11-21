package httpx

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"
)

// writeResponse sends a finalized response; HEAD gets the GET headers and
// no body.
func writeResponse(w http.ResponseWriter, req *http.Request, r *Response) {
	if r.hasValue {
		r.encodeValue()
	}
	if r.Status == 0 {
		r.Status = 200
	}
	h := w.Header()
	for key, values := range r.Header() {
		h[key] = values
	}
	switch {
	case r.file != nil:
		writeFile(w, req, r)
	case r.stream != nil:
		w.WriteHeader(r.Status)
		if req.Method != "HEAD" {
			_ = r.stream(w)
		}
	default:
		h.Set("Content-Length", strconv.Itoa(len(r.body)))
		if h.Get("Content-Type") == "" && len(r.body) > 0 {
			h["Content-Type"] = nil // never sniff
		}
		w.WriteHeader(r.Status)
		if req.Method != "HEAD" {
			_, _ = w.Write(r.body)
		}
	}
}

// writeFile sends a file response with http.ServeContent, which handles
// HEAD, Range (206/416), If-Range, If-None-Match and If-Modified-Since
// against the ETag and Last-Modified headers already set by File. Only GET
// and HEAD 200 answers go through it: a file returned by a POST route (a
// generated download) or with another status is sent whole, without range
// or precondition handling (RFC 9110 defines Range for GET only).
func writeFile(w http.ResponseWriter, req *http.Request, r *Response) {
	f := r.file
	h := w.Header()
	var content io.ReadSeeker
	if f.options.Path != "" {
		file, err := os.Open(f.options.Path)
		if err != nil {
			page := (&HTTPError{Code: 500, Description: DescInternal}).response(req.URL.Path)
			for key := range h {
				h.Del(key)
			}
			for key, values := range page.Header() {
				h[key] = values
			}
			h.Set("Content-Length", strconv.Itoa(len(page.body)))
			w.WriteHeader(500)
			if req.Method != "HEAD" {
				_, _ = w.Write(page.body)
			}
			return
		}
		defer file.Close()
		content = file
	} else {
		content = bytes.NewReader(f.data)
	}
	if r.Status != http.StatusOK || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
		h.Set("Content-Length", strconv.FormatInt(f.size, 10))
		w.WriteHeader(r.Status)
		if req.Method != "HEAD" {
			_, _ = io.CopyN(w, content, f.size)
		}
		return
	}
	http.ServeContent(w, req, "", f.modTime, content)
}
