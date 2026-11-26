package httpx

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/weburl"
)

// Edge is the web front end that docker/nginx.conf provided: it serves the
// built frontend (frontend/dist) with the same caching and SPA rules, the
// /health and /_configflow/listener-health probes, and passes /api and /mcp
// to the App with nginx's proxy headers and body limits. Every location of
// nginx.conf is reproduced:
//
//	= /frontend-version.json                 file or plain 404, no-store
//	~ ^/assets/<name>-<hash 8+>.<ext>$        file or plain 404, immutable for a year
//	/assets/                                  file or plain 404, no-cache
//	~ static extensions outside /api, /mcp    file or plain 404, no-cache
//	~ ^/api/(profiles/x/)?oxidns/(settings|render)$   API, 96 MiB body
//	/api/                                     API, 1 MiB body
//	/mcp                                      API, 1 MiB body
//	= /_configflow/listener-health            the listener token, no-store
//	/health                                   "healthy"
//	/                                         file, directory index or /index.html (SPA), no-cache
//
// plus gzip_static (a precompressed .gz next to the file), gzip for the
// listed types, ETag/Last-Modified with nginx's exact If-Modified-Since,
// and an access log of 4xx/5xx responses on stdout.
type Edge struct {
	// API is the application (*App), possibly wrapped.
	API http.Handler
	// Root is the frontend build directory (nginx root).
	Root string
	// AccessLog receives nginx-style lines for 4xx/5xx; nil disables.
	AccessLog io.Writer

	token atomic.Value // string
}

// Body limits of nginx.conf.
const (
	DefaultBodyLimit = 1 << 20
	OxidnsBodyLimit  = 96 << 20
)

// SetListenerToken sets what /_configflow/listener-health answers.
func (e *Edge) SetListenerToken(token string) { e.token.Store(token) }

// ListenerToken returns the current listener token.
func (e *Edge) ListenerToken() string {
	token, _ := e.token.Load().(string)
	return token
}

var (
	hashedAsset   = regexp.MustCompile(`^/assets/.+-[A-Za-z0-9_-]{8,}\.[^/]+$`)
	staticExt     = regexp.MustCompile(`^/.*\.(?:m?js|css|json|map|webmanifest|ico|png|svg|woff2?)$`)
	oxidnsUpload  = regexp.MustCompile(`^/api/(?:profiles/[^/]+/)?oxidns/(?:settings|render)$`)
	apiOrMCPStart = regexp.MustCompile(`^/(?:api|mcp)(?:/|$)`)
)

// ServeHTTP dispatches like nginx's location matching.
func (e *Edge) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	logged := &statusWriter{ResponseWriter: w, status: 200}
	defer e.logAccess(logged, req)
	uri := normalizeURI(req.URL.Path)
	switch uri {
	case "/frontend-version.json":
		e.static(logged, req, uri, "no-store", true)
		return
	case "/_configflow/listener-health":
		h := logged.Header()
		h.Set("Content-Type", "text/plain")
		h.Set("Cache-Control", "no-store")
		body := e.ListenerToken() + "\n"
		h.Set("Content-Length", strconv.Itoa(len(body)))
		logged.WriteHeader(200)
		if req.Method != "HEAD" {
			io.WriteString(logged, body)
		}
		return
	}
	// Regular expression locations win over prefix locations.
	switch {
	case hashedAsset.MatchString(uri):
		e.static(logged, req, uri, "public, max-age=31536000, immutable", false)
		return
	case staticExt.MatchString(uri) && !apiOrMCPStart.MatchString(uri):
		e.static(logged, req, uri, "no-cache", true)
		return
	case oxidnsUpload.MatchString(uri):
		e.proxy(logged, req, OxidnsBodyLimit)
		return
	}
	switch {
	case strings.HasPrefix(uri, "/api/"), strings.HasPrefix(uri, "/mcp"):
		e.proxy(logged, req, DefaultBodyLimit)
	case strings.HasPrefix(uri, "/health"):
		h := logged.Header()
		h.Set("Content-Type", "text/plain")
		h.Set("Content-Length", "8")
		logged.WriteHeader(200)
		if req.Method != "HEAD" {
			io.WriteString(logged, "healthy\n")
		}
	case strings.HasPrefix(uri, "/assets/"):
		e.static(logged, req, uri, "no-cache", true)
	default:
		e.spa(logged, req, uri)
	}
}

// normalizeURI is nginx's URI normalisation for location matching: decoded,
// "." and ".." resolved, repeated slashes merged.
func normalizeURI(p string) string {
	if p == "" {
		return "/"
	}
	trailing := strings.HasSuffix(p, "/")
	clean := path.Clean("/" + p)
	if trailing && clean != "/" {
		clean += "/"
	}
	return clean
}

// proxy passes the request to the App as nginx's proxy_pass did.
func (e *Edge) proxy(w http.ResponseWriter, req *http.Request, limit int64) {
	if !limitRequestBody(w, req, limit) {
		return
	}
	host := nginxHost(req)
	out := req.Clone(context.WithValue(req.Context(), edgeOriginKey{}, requestOrigin(req)))
	out.Host = host
	out.Header.Set("Host", host)
	client := remoteIP(req.RemoteAddr)
	out.Header.Set("X-Real-IP", client)
	if previous := strings.Join(req.Header.Values("X-Forwarded-For"), ", "); previous != "" {
		out.Header.Set("X-Forwarded-For", previous+", "+client)
	} else {
		out.Header.Set("X-Forwarded-For", client)
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	out.Header.Set("X-Forwarded-Proto", scheme)
	out.Header.Set("X-Forwarded-Host", host)
	out.Header.Del("Connection")
	out.RemoteAddr = "127.0.0.1:0"
	out.TLS = nil
	gz := newGzipWriter(w, req)
	defer gz.Close()
	e.API.ServeHTTP(gz, out)
}

// nginxHost is $host: the Host header without port, lower-cased.
func nginxHost(req *http.Request) string {
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	host = strings.ToLower(host)
	if host == "" {
		host = "_"
	}
	return host
}

func remoteIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// static is try_files $uri @static_not_found with a Cache-Control header.
// always: add_header ... always (also on errors); otherwise only on success.
func (e *Edge) static(w http.ResponseWriter, req *http.Request, uri, cacheControl string, always bool) {
	file := e.resolve(uri)
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		h := w.Header()
		h.Set("Content-Type", "text/plain")
		h.Set("Cache-Control", "no-store")
		body := "Static file not found\n"
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(404)
		if req.Method != "HEAD" {
			io.WriteString(w, body)
		}
		return
	}
	e.serveFile(w, req, file, info, cacheControl)
}

// spa is location / { try_files $uri $uri/ /index.html; }.
func (e *Edge) spa(w http.ResponseWriter, req *http.Request, uri string) {
	file := e.resolve(uri)
	info, err := os.Stat(file)
	if err == nil && info.IsDir() {
		index := filepath.Join(file, "index.html")
		if indexInfo, err := os.Stat(index); err == nil && indexInfo.Mode().IsRegular() {
			e.serveFile(w, req, index, indexInfo, "no-cache")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		nginxError(w, req, http.StatusForbidden)
		return
	}
	if err == nil && info.Mode().IsRegular() {
		e.serveFile(w, req, file, info, "no-cache")
		return
	}
	index := filepath.Join(e.Root, "index.html")
	if indexInfo, err := os.Stat(index); err == nil && indexInfo.Mode().IsRegular() {
		e.serveFile(w, req, index, indexInfo, "no-cache")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	nginxError(w, req, http.StatusInternalServerError)
}

func (e *Edge) resolve(uri string) string {
	return filepath.Join(e.Root, filepath.FromSlash(path.Clean("/"+uri)))
}

// serveFile sends a static file: gzip_static, nginx ETag and exact
// If-Modified-Since, ranges, Cache-Control.
func (e *Edge) serveFile(w http.ResponseWriter, req *http.Request, file string, info os.FileInfo, cacheControl string) {
	h := w.Header()
	h.Set("Content-Type", NginxMimeType(file))
	if cacheControl != "" {
		h.Set("Cache-Control", cacheControl)
	}
	if acceptsGzip(req) {
		if gzInfo, err := os.Stat(file + ".gz"); err == nil && gzInfo.Mode().IsRegular() {
			h.Set("Content-Encoding", "gzip")
			h.Add("Vary", "Accept-Encoding")
			file, info = file+".gz", gzInfo
		} else if compressible(h.Get("Content-Type")) && info.Size() >= 20 {
			h.Add("Vary", "Accept-Encoding")
			e.serveCompressed(w, req, file, info)
			return
		}
	}
	modTime := info.ModTime().UTC().Truncate(time.Second)
	lastModified := modTime.Format(http.TimeFormat)
	etag := fmt.Sprintf(`"%x-%x"`, modTime.Unix(), info.Size())
	h.Set("Last-Modified", lastModified)
	h.Set("ETag", etag)
	if req.Header.Get("If-None-Match") == "" {
		if since := req.Header.Get("If-Modified-Since"); since != "" {
			if t, err := http.ParseTime(since); err == nil && t.Equal(modTime) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	f, err := os.Open(file)
	if err != nil {
		nginxError(w, req, http.StatusForbidden)
		return
	}
	defer f.Close()
	// ServeContent handles If-None-Match against the ETag and byte ranges;
	// the zero time keeps it from applying its own If-Modified-Since rule.
	http.ServeContent(w, req, "", time.Time{}, f)
}

func (e *Edge) serveCompressed(w http.ResponseWriter, req *http.Request, file string, info os.FileInfo) {
	h := w.Header()
	h.Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	h.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, info.ModTime().Unix(), info.Size()))
	if match := req.Header.Get("If-None-Match"); match != "" && strings.Contains(match, fmt.Sprintf(`"%x-%x"`, info.ModTime().Unix(), info.Size())) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		nginxError(w, req, http.StatusForbidden)
		return
	}
	defer f.Close()
	h.Set("Content-Encoding", "gzip")
	w.WriteHeader(200)
	if req.Method == "HEAD" {
		return
	}
	gz, _ := gzip.NewWriterLevel(w, 6)
	_, _ = io.Copy(gz, f)
	_ = gz.Close()
}

// nginxError writes nginx's default error page.
func nginxError(w http.ResponseWriter, req *http.Request, status int) {
	title := strconv.Itoa(status) + " " + reasonPhrase(status)
	body := "<html>\r\n<head><title>" + title + "</title></head>\r\n<body>\r\n<center><h1>" + title +
		"</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"
	h := w.Header()
	h.Set("Content-Type", "text/html")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if status == http.StatusRequestEntityTooLarge {
		h.Set("Connection", "close")
	}
	w.WriteHeader(status)
	if req.Method != "HEAD" {
		io.WriteString(w, body)
	}
}

// nginxTypes is the part of nginx's mime.types the frontend uses.
var nginxTypes = map[string]string{
	".html": "text/html", ".htm": "text/html", ".css": "text/css", ".xml": "text/xml",
	".js": "application/javascript", ".mjs": "application/javascript", ".json": "application/json",
	".webmanifest": "application/manifest+json", ".txt": "text/plain", ".png": "image/png",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".svg": "image/svg+xml",
	".svgz": "image/svg+xml", ".ico": "image/x-icon", ".webp": "image/webp", ".woff": "font/woff",
	".woff2": "font/woff2", ".wasm": "application/wasm", ".zip": "application/zip",
	".pdf": "application/pdf", ".avif": "image/avif",
}

// NginxMimeType is nginx's type for a file name (default_type
// application/octet-stream).
func NginxMimeType(name string) string {
	if t, ok := nginxTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

var gzipTypes = map[string]bool{
	"text/html": true, "text/plain": true, "text/css": true, "text/xml": true, "text/javascript": true,
	"application/json": true, "application/javascript": true, "application/xml+rss": true,
	"application/rss+xml": true, "font/truetype": true, "font/opentype": true,
	"application/vnd.ms-fontobject": true, "image/svg+xml": true,
}

func compressible(contentType string) bool {
	mimetype, _, _ := strings.Cut(contentType, ";")
	return gzipTypes[strings.ToLower(strings.TrimSpace(mimetype))]
}

func acceptsGzip(req *http.Request) bool {
	for _, part := range strings.Split(req.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			params = strings.ReplaceAll(params, " ", "")
			return params != "q=0" && params != "q=0.0" && params != "q=0.00" && params != "q=0.000"
		}
	}
	return false
}

// gzipWriter compresses proxied responses like nginx's gzip module.
type gzipWriter struct {
	http.ResponseWriter
	req     *http.Request
	accept  bool
	decided bool
	gz      *gzip.Writer
}

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, 6); return w }}

func newGzipWriter(w http.ResponseWriter, req *http.Request) *gzipWriter {
	return &gzipWriter{ResponseWriter: w, req: req, accept: acceptsGzip(req)}
}

func (g *gzipWriter) WriteHeader(status int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		length, lengthErr := strconv.Atoi(h.Get("Content-Length"))
		if g.accept && g.req.Method != "HEAD" && status != 204 && status != 304 && status >= 200 &&
			h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type")) && (lengthErr != nil || length >= 20) {
			h.Del("Content-Length")
			h.Set("Content-Encoding", "gzip")
			if etag := h.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
				h.Set("ETag", "W/"+etag)
			}
			g.gz = gzipPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
		if g.accept || compressible(h.Get("Content-Type")) {
			if g.gz != nil {
				h.Add("Vary", "Accept-Encoding")
			}
		}
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(200)
	}
	if g.gz != nil {
		return g.gz.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) Close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (s *statusWriter) WriteHeader(status int) {
	if !s.wrote {
		s.status, s.wrote = status, true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(200)
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytes += int64(n)
	return n, err
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logAccess writes nginx's "main" log format for 4xx and 5xx responses.
func (e *Edge) logAccess(s *statusWriter, req *http.Request) {
	if e.AccessLog == nil || s.status < 400 {
		return
	}
	dash := func(v string) string {
		if v == "" {
			return "-"
		}
		return v
	}
	line := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d \"%s\" \"%s\" \"%s\"\n",
		remoteIP(req.RemoteAddr), time.Now().Format("02/Jan/2006:15:04:05 -0700"), req.Method, weburl.SafeForLog(req.RequestURI),
		req.Proto, s.status, s.bytes, dash(weburl.SafeForLog(req.Referer())), dash(req.UserAgent()),
		dash(req.Header.Get("X-Forwarded-For")))
	_, _ = io.WriteString(e.AccessLog, line)
}
