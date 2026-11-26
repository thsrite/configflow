package oxidns

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// SettingsError is a rejected OxiDNS setting (Python OxidnsSettingsError);
// Msg is shown to the user. Issues, when set, are the structured diagnostics
// of a structural native-document error.
type SettingsError struct {
	Msg    string
	Issues []Issue
}

func (e *SettingsError) Error() string { return e.Msg }

func fail(msg string) error { return &SettingsError{Msg: msg} }

// ValidationError is a native document with error issues (Python
// OxidnsValidationError, a SettingsError subclass).
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string {
	for _, item := range e.Issues {
		if item.Severity == "" || item.Severity == "error" {
			return "OxiDNS 配置校验未通过：" + item.Path.String() + " " + item.Message
		}
	}
	return "OxiDNS 配置校验未通过：config.yaml 请检查参数"
}

// ReferenceError is OxidnsReferenceError: a feature still referenced.
type ReferenceError struct {
	Msg        string
	References []Reference
}

func (e *ReferenceError) Error() string { return e.Msg }

// NewReferenceError appends up to four locations to message.
func NewReferenceError(message string, references []Reference) *ReferenceError {
	var locations []string
	for i, item := range references {
		if i == 4 {
			break
		}
		locations = append(locations, item.Path.dotted())
	}
	if len(locations) > 0 {
		message += "（" + strings.Join(locations, "、") + "）"
	}
	return &ReferenceError{Msg: message, References: references}
}

// IsSettingsError reports whether err is a user-facing rejection (any of the
// three types above) and returns its message.
func IsSettingsError(err error) (string, bool) {
	var s *SettingsError
	var v *ValidationError
	var r *ReferenceError
	switch {
	case errors.As(err, &s):
		return s.Msg, true
	case errors.As(err, &v):
		return v.Error(), true
	case errors.As(err, &r):
		return r.Msg, true
	}
	return "", false
}

// errMalformed reports input Python's code would crash on (TypeError,
// AttributeError, KeyError): rejected, but not a user-facing message.
var errMalformed = errors.New("oxidns: malformed value")

// Issue is one validation diagnostic.
type Issue struct {
	Path     Path   `json:"path"`
	Message  string `json:"message"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Source   string `json:"source,omitempty"`
}

// Path addresses a value: mapping keys and sequence indexes.
type Path []Elem

// Elem is a key, or an index when IsIndex.
type Elem struct {
	Key     string
	Index   int
	IsIndex bool
}

func (e Elem) MarshalJSON() ([]byte, error) {
	if e.IsIndex {
		return []byte(strconv.Itoa(e.Index)), nil
	}
	return json.Marshal(e.Key)
}

func (e *Elem) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &e.Key)
	}
	e.IsIndex = true
	return json.Unmarshal(data, &e.Index)
}

func (p Path) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]Elem(p))
}

// P builds a path from strings and ints.
func P(parts ...any) Path {
	out := make(Path, 0, len(parts))
	for _, part := range parts {
		switch x := part.(type) {
		case string:
			out = append(out, Elem{Key: x})
		case int:
			out = append(out, Elem{Index: x, IsIndex: true})
		case Path:
			out = append(out, x...)
		}
	}
	return out
}

// with returns a copy of p extended by parts.
func (p Path) with(parts ...any) Path { return P(append([]any{p}, parts...)...) }

// String is format_path: a.b[0].c, or config.yaml for the root.
func (p Path) String() string {
	if s := p.dotted(); s != "" {
		return s
	}
	return "config.yaml"
}

func (p Path) dotted() string {
	var b strings.Builder
	for i, e := range p {
		if e.IsIndex {
			b.WriteString("[" + strconv.Itoa(e.Index) + "]")
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(e.Key)
	}
	return b.String()
}

func hasError(issues []Issue) bool {
	for _, item := range issues {
		if item.Severity == "error" {
			return true
		}
	}
	return false
}

// errorsFirst is sorted(..., key=severity != 'error') (stable).
func errorsFirst(issues []Issue) []Issue {
	out := make([]Issue, 0, len(issues))
	for _, pass := range []bool{true, false} {
		for _, item := range issues {
			if (item.Severity == "error") == pass {
				out = append(out, item)
			}
		}
	}
	return out
}
