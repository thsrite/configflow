package httpx

import (
	"hash/adler32"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"configflow/internal/jsonx"
)

// Response is what a handler returns: Flask's Response object. Build it with
// JSON, JSONStatus, External, Text, HTML, Bytes, File or Empty, then adjust
// with WithStatus / WithHeader.
type Response struct {
	Status int
	header http.Header

	body []byte
	// value is encoded before the global JSON response policy is applied.
	value    any
	hasValue bool

	// passthrough is Werkzeug's direct_passthrough (send_file): the response
	// hooks never parse or rewrite the body.
	passthrough bool
	file        *fileBody
	// stream writes the body (for Stream responses); nil otherwise.
	stream func(w io.Writer) error

	// sortValue preserves Flask's original-key order for redaction collisions.
	sortValue bool
}

// Header returns the response headers (mutable).
func (r *Response) Header() http.Header {
	if r.header == nil {
		r.header = http.Header{}
	}
	return r.header
}

// WithStatus sets the status code and returns r.
func (r *Response) WithStatus(status int) *Response { r.Status = status; return r }

// WithHeader sets a header and returns r (response.headers[k] = v).
func (r *Response) WithHeader(key, value string) *Response {
	r.Header().Set(key, value)
	return r
}

// NoStore sets Cache-Control: no-store and returns r.
func (r *Response) NoStore() *Response { return r.WithHeader("Cache-Control", "no-store") }

// JSON returns v as JSON with status 200. Native Go values and legacy jsonx
// values use the same encoding/json response pipeline.
func JSON(v any) *Response { return JSONStatus(200, v) }

// JSONStatus is `return jsonify(v), status`.
func JSONStatus(status int, v any) *Response {
	r := &Response{Status: status, value: v, hasValue: true, sortValue: true}
	r.Header().Set("Content-Type", "application/json")
	return r
}

// Message is the common {"success": false, "message": msg} body with status.
func Message(status int, success bool, message string) *Response {
	return JSONStatus(status, jsonx.Obj("success", success, "message", message))
}

// Bytes is a raw response with a content type.
func Bytes(status int, contentType string, body []byte) *Response {
	r := &Response{Status: status, body: body}
	if contentType != "" {
		r.Header().Set("Content-Type", contentType)
	}
	return r
}

// Text is Response(text, mimetype="text/plain") (charset utf-8 added as
// Werkzeug does for text types).
func Text(status int, text string) *Response {
	return Bytes(status, "text/plain; charset=utf-8", []byte(text))
}

// HTML is what Flask makes of a view returning a plain str.
func HTML(status int, text string) *Response {
	return Bytes(status, "text/html; charset=utf-8", []byte(text))
}

// RawJSON is Response(body, mimetype="application/json"): already encoded
// JSON; the response hook still parses and redacts it.
func RawJSON(status int, body []byte) *Response {
	return Bytes(status, "application/json", body)
}

// Empty is a body-less response (e.g. 204), Content-Type text/html as
// Flask's default.
func Empty(status int) *Response {
	return Bytes(status, "text/html; charset=utf-8", nil)
}

// Stream is a streamed body (Flask: Response(generator)). The hooks do not
// touch it; Content-Length is not set.
func Stream(status int, contentType string, write func(w io.Writer) error) *Response {
	r := &Response{Status: status, stream: write, passthrough: true}
	r.Header().Set("Content-Type", contentType)
	return r
}

// Passthrough marks the response as direct_passthrough: hooks leave the body
// alone (Werkzeug sets this for send_file).
func (r *Response) Passthrough() *Response { r.passthrough = true; return r }

// IsJSON is werkzeug's Response.is_json.
func (r *Response) IsJSON() bool {
	mimetype, _, _ := strings.Cut(r.Header().Get("Content-Type"), ";")
	mimetype = strings.ToLower(strings.TrimSpace(mimetype))
	return mimetype == "application/json" ||
		strings.HasPrefix(mimetype, "application/") && strings.HasSuffix(mimetype, "+json")
}

// Value returns the pending jsonify payload (ok false for encoded bodies).
func (r *Response) Value() (any, bool) { return r.value, r.hasValue }

// Body returns the encoded body (jsonify payloads are encoded now).
func (r *Response) Body() []byte {
	if r.hasValue {
		r.encodeValue()
	}
	return r.body
}

// SetBody replaces the body (response.set_data).
func (r *Response) SetBody(body []byte) {
	r.body, r.value, r.hasValue, r.sortValue = body, nil, false, false
}

func (r *Response) encodeValue() {
	if err := writeJSON(nil, r); err != nil {
		*r = *(&HTTPError{Code: 500, Description: DescInternal}).response("/api/")
	}
}

// FileOptions describe a send_file response.
type FileOptions struct {
	// Path of a file on disk, or Data for an in-memory file (BytesIO).
	Path string
	Data []byte
	// MimeType; empty guesses from DownloadName (or Path) like Flask.
	MimeType string
	// AsAttachment adds Content-Disposition: attachment with DownloadName.
	AsAttachment bool
	DownloadName string
	// NoETag disables the ETag Werkzeug derives for files on disk.
	NoETag bool
	// MaxAge, when set, replaces Cache-Control: no-cache with public, max-age.
	MaxAge *int
}

type fileBody struct {
	options FileOptions
	size    int64
	modTime time.Time
	data    []byte
}

// File is flask.send_file. The response is conditional (ETag and
// Last-Modified for files on disk, If-None-Match / If-Modified-Since → 304,
// single byte ranges → 206) and passes the response hooks untouched.
// A missing file gives a 404 error (Flask: FileNotFoundError → 500; check
// existence in the handler where Python did).
func File(options FileOptions) (*Response, error) {
	body := &fileBody{options: options}
	if options.Path != "" {
		info, err := os.Stat(options.Path)
		if err != nil {
			return nil, err
		}
		body.size, body.modTime = info.Size(), info.ModTime()
	} else {
		body.data = options.Data
		body.size = int64(len(options.Data))
	}
	r := &Response{Status: 200, passthrough: true, file: body}
	mimetype := options.MimeType
	if mimetype == "" {
		name := options.DownloadName
		if name == "" {
			name = options.Path
		}
		mimetype = GuessMimeType(name)
	}
	if strings.HasPrefix(mimetype, "text/") && !strings.Contains(mimetype, "charset") {
		mimetype += "; charset=utf-8"
	}
	r.Header().Set("Content-Type", mimetype)
	if options.AsAttachment || options.DownloadName != "" {
		disposition := "inline"
		if options.AsAttachment {
			disposition = "attachment"
		}
		name := options.DownloadName
		if name == "" {
			name = options.Path[strings.LastIndexByte(options.Path, '/')+1:]
		}
		r.Header().Set("Content-Disposition", contentDisposition(disposition, name))
	}
	if options.MaxAge != nil {
		if *options.MaxAge > 0 {
			r.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(*options.MaxAge))
		} else {
			r.Header().Set("Cache-Control", "no-cache, max-age="+strconv.Itoa(*options.MaxAge))
		}
		r.Header().Set("Expires", time.Now().Add(time.Duration(*options.MaxAge)*time.Second).UTC().Format(http.TimeFormat))
	} else {
		r.Header().Set("Cache-Control", "no-cache")
	}
	if options.Path != "" {
		r.Header().Set("Last-Modified", body.modTime.UTC().Format(http.TimeFormat))
		if !options.NoETag {
			mtime := float64(body.modTime.UnixNano()) / 1e9
			check := adler32.Checksum([]byte(options.Path))
			r.Header().Set("ETag", `"`+jsonx.Float(mtime).String()+"-"+strconv.FormatInt(body.size, 10)+"-"+
				strconv.FormatUint(uint64(check), 10)+`"`)
		}
	}
	return r, nil
}

// contentDisposition is werkzeug's send_file header: a plain ASCII token
// filename, quoted when needed, with filename* for non-ASCII names.
func contentDisposition(disposition, name string) string {
	ascii := true
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return disposition + "; filename=" + quoteHeaderValue(name)
	}
	simple := strings.Map(func(r rune) rune {
		if r < 0x80 {
			return r
		}
		return -1
	}, name)
	return disposition + "; filename=" + quoteHeaderValue(simple) + "; filename*=UTF-8''" + quoteURL(name)
}

const tokenChars = "!#$%&'*+-.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ^_`abcdefghijklmnopqrstuvwxyz|~"

// quoteHeaderValue is werkzeug.http.quote_header_value.
func quoteHeaderValue(value string) string {
	if value == "" {
		return `""`
	}
	plain := true
	for i := 0; i < len(value); i++ {
		if !strings.ContainsRune(tokenChars, rune(value[i])) {
			plain = false
			break
		}
	}
	if plain {
		return value
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// quoteURL is urllib.parse.quote(value, safe="!#$&+^`|") as werkzeug uses it.
func quoteURL(value string) string {
	const safe = "!#$&+^`|"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("_.-~/", c) >= 0 ||
			strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)>>4, 16)) + strings.ToUpper(strconv.FormatUint(uint64(c)&15, 16)))
		}
	}
	return b.String()
}

// mimeTypes is Python's mimetypes table for the extensions this app
// downloads (Python 3.11 without /etc/mime.types).
var mimeTypes = map[string]string{
	".json": "application/json", ".zip": "application/zip", ".yaml": "application/yaml",
	".yml": "application/yaml", ".txt": "text/plain", ".conf": "application/octet-stream",
	".sh": "application/x-sh", ".gz": "application/gzip", ".tar": "application/x-tar",
	".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
	".svg": "image/svg+xml", ".bin": "application/octet-stream",
}

// GuessMimeType is Flask's mimetypes.guess_type fallback for send_file:
// application/octet-stream when the extension is unknown.
func GuessMimeType(name string) string {
	ext := strings.ToLower(name[strings.LastIndexByte(name, '.')+1:])
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		ext = strings.ToLower(name[i:])
		if t, ok := mimeTypes[ext]; ok {
			return t
		}
		if t := mime.TypeByExtension(ext); t != "" {
			t, _, _ = strings.Cut(t, ";")
			return t
		}
	}
	return "application/octet-stream"
}
