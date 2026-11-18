package httpx

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// writeResponse sends a finalized response, applying send_file's
// conditional handling and Werkzeug's HEAD behaviour (headers of the GET
// response, no body).
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

func writeFile(w http.ResponseWriter, req *http.Request, r *Response) {
	f := r.file
	h := w.Header()
	if req.Method == "GET" || req.Method == "HEAD" {
		h.Set("Accept-Ranges", "bytes")
	}
	if notModified(req, h, f.modTime, f.options.Path != "") {
		for _, key := range []string{"Content-Type", "Content-Length", "Content-Disposition"} {
			h.Del(key)
		}
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var reader io.ReadSeeker
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
		reader = file
	} else {
		reader = bytes.NewReader(f.data)
	}
	start, end, status := int64(0), f.size, r.Status
	if rng := req.Header.Get("Range"); rng != "" && (req.Method == "GET" || req.Method == "HEAD") && r.Status == 200 && ifRangeMatches(req, h, f.modTime) {
		if s, e, ok := parseRange(rng, f.size); ok {
			start, end, status = s, e, http.StatusPartialContent
			h.Set("Content-Range", "bytes "+strconv.FormatInt(s, 10)+"-"+strconv.FormatInt(e-1, 10)+"/"+strconv.FormatInt(f.size, 10))
		} else if s < 0 {
			h.Set("Content-Range", "bytes */"+strconv.FormatInt(f.size, 10))
			page := (&HTTPError{Code: 416, Description: "The server cannot provide the requested range."}).response(req.URL.Path)
			h.Set("Content-Type", page.Header().Get("Content-Type"))
			h.Set("Content-Length", strconv.Itoa(len(page.body)))
			h.Del("Content-Disposition")
			w.WriteHeader(416)
			if req.Method != "HEAD" {
				_, _ = w.Write(page.body)
			}
			return
		}
	}
	h.Set("Content-Length", strconv.FormatInt(end-start, 10))
	w.WriteHeader(status)
	if req.Method == "HEAD" {
		return
	}
	if _, err := reader.Seek(start, io.SeekStart); err == nil {
		_, _ = io.CopyN(w, reader, end-start)
	}
}

// notModified is Werkzeug's is_resource_modified check of make_conditional.
func notModified(req *http.Request, h http.Header, modTime time.Time, hasTime bool) bool {
	if req.Method != "GET" && req.Method != "HEAD" {
		return false
	}
	etag := h.Get("ETag")
	if match := req.Header.Get("If-None-Match"); match != "" {
		if etag == "" {
			return false
		}
		bare := strings.TrimPrefix(etag, "W/")
		for _, candidate := range strings.Split(match, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || strings.TrimPrefix(candidate, "W/") == bare {
				return true
			}
		}
		return false
	}
	if since := req.Header.Get("If-Modified-Since"); since != "" && hasTime {
		t, err := http.ParseTime(since)
		if err == nil && !modTime.Truncate(time.Second).After(t) {
			return true
		}
	}
	return false
}

func ifRangeMatches(req *http.Request, h http.Header, modTime time.Time) bool {
	value := req.Header.Get("If-Range")
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "W/") {
		return value == h.Get("ETag")
	}
	t, err := http.ParseTime(value)
	return err == nil && !modTime.Truncate(time.Second).After(t)
}

// parseRange parses a single "bytes=" range. ok false with start < 0 means
// unsatisfiable (416); ok false otherwise means "ignore the header".
func parseRange(value string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(value), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	if first == "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		if size == 0 {
			return -1, 0, false
		}
		return size - n, size, true
	}
	s, err := strconv.ParseInt(first, 10, 64)
	if err != nil || s < 0 {
		return 0, 0, false
	}
	e := size
	if last != "" {
		v, err := strconv.ParseInt(last, 10, 64)
		if err != nil || v < s {
			return 0, 0, false
		}
		e = min(v+1, size)
	}
	if s >= size {
		return -1, 0, false
	}
	return s, e, true
}
