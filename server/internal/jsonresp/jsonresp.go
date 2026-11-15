// Package jsonresp is the pure part of backend/common/json_response.py: the
// byte-exact encoding behind jsonify_external and the check that lets the
// global response hook skip a second redaction. The HTTP glue (building the
// response, content type, running the redaction itself) belongs to
// internal/httpx and internal/api.
//
// What the httpx package has to add around this one:
//
//   - the secrets: config_export._external_payload_secrets() and
//     _sanitize_with_secrets(payload, secrets, sort_dict_keys=True) (package
//     #17); External takes the already redacted payload;
//   - the response: body = External(sanitized) with Content-Type
//     "application/json" and no trailing newline (unlike flask.jsonify);
//   - the marker: keep the Sanitized value next to the response (a field of the
//     response wrapper, never a header) and call Consume from the global
//     response hook, which skips redaction only when it returns true;
//   - the fallback: when PlainTree says false, use the ordinary jsonify path
//     (jsonx.Response) and let the hook redact.
package jsonresp

import (
	"bytes"
	"slices"
	"unsafe"

	"configflow/internal/jsonx"
)

// maxDepth is the traversal bound of _plain_json_tree (`len(stack) > 128`).
const maxDepth = 128

// External is `current_app.json.dumps(payload)` with Flask's default provider:
// json.dumps(payload, ensure_ascii=True, sort_keys=True) with the ", " and ": "
// separators and no trailing newline.
func External(payload any) ([]byte, error) {
	return jsonx.Encode(payload, jsonx.Options{EnsureASCII: true, SortKeys: true})
}

// PlainTree is _plain_json_tree: whether the payload is an ordinary JSON
// document whose encoding cannot differ between the one-pass and the global
// path: an object or list at the root, nesting no deeper than 128 containers,
// and no container that contains itself. (Python also refused non-str keys,
// non-JSON types and surrogate strings; the jsonx model cannot hold those.)
func PlainTree(payload any) bool {
	switch payload.(type) {
	case *jsonx.Object, []any:
	default:
		return false
	}
	active := map[unsafe.Pointer]bool{}
	return plain(payload, 1, active)
}

func plain(value any, depth int, active map[unsafe.Pointer]bool) bool {
	var id unsafe.Pointer
	var children []any
	switch v := value.(type) {
	case *jsonx.Object:
		if v == nil {
			return true
		}
		id = unsafe.Pointer(v)
		children = v.Values()
	case []any:
		// Python checks the depth of every list, empty ones included.
		if depth > maxDepth {
			return false
		}
		if len(v) == 0 {
			return true
		}
		id = unsafe.Pointer(unsafe.SliceData(v))
		children = v
	default:
		return true
	}
	if depth > maxDepth || active[id] {
		return false
	}
	active[id] = true
	defer delete(active, id)
	for _, child := range children {
		if !plain(child, depth+1, active) {
			return false
		}
	}
	return true
}

// Sanitized is _SanitizedResponse: the private note kept with a response whose
// body was already redacted against the credentials current at the time.
type Sanitized struct {
	Body     []byte
	Secrets  []string
	Endpoint string
}

// NewSanitized records the body, the credentials it was redacted against and
// the endpoint that produced it.
func NewSanitized(body []byte, secrets []string, endpoint string) *Sanitized {
	return &Sanitized{Body: slices.Clone(body), Secrets: slices.Clone(secrets), Endpoint: endpoint}
}

// Consume is consume_sanitized_response: true only when the response is still
// for the same endpoint, its body is byte for byte what was redacted, and the
// credentials have not changed since (a later hook may have edited the body, a
// request may have rotated tokens; both need the global redaction). A nil
// marker, as for a response that was never prepared here, gives false.
func (s *Sanitized) Consume(body []byte, endpoint string, secrets []string) bool {
	if s == nil {
		return false
	}
	return s.Endpoint == endpoint && bytes.Equal(s.Body, body) && slices.Equal(s.Secrets, secrets)
}
