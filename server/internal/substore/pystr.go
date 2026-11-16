package substore

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// PyError is a Python exception raised by the ported code where Python
// raises one that is not one of this package's own classes (ValueError,
// TypeError, AttributeError, OverflowError, UnicodeDecodeError, OSError
// subclasses …). Class is type(exc).__name__, Message str(exc).
type PyError struct {
	Class   string
	Message string
	Err     error
}

func (e *PyError) Error() string       { return e.Message }
func (e *PyError) Unwrap() error       { return e.Err }
func (e *PyError) PythonClass() string { return e.Class }

func pyErr(class, message string) *PyError { return &PyError{Class: class, Message: message} }

// pythonClasser is implemented by errors that know their Python class.
type pythonClasser interface{ PythonClass() string }

// statusCarrier is implemented by errors with a response status
// (exc.response.status_code).
type statusCarrier interface{ HTTPStatus() int }

// pythonClass is type(err).__name__ for errors of this package; other Go
// errors are reported by their Go type name like urlx.SafeExceptionDetails.
func pythonClass(err error) string {
	if c, ok := err.(pythonClasser); ok {
		return c.PythonClass()
	}
	return fmt.Sprintf("%T", err)
}

// safeExceptionDetails is url_utils.safe_exception_details: the exception
// class (characters outside [A-Za-z0-9_.-] removed, at most 80) and the
// status of exc.response when it has one. Only err itself is inspected, as
// Python reads attributes of the exception object only.
func safeExceptionDetails(err error) string {
	var b strings.Builder
	for _, r := range pythonClass(err) {
		if r < 128 && (r == '_' || r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "Exception"
	}
	details := "exception_type=" + name
	if s, ok := err.(statusCarrier); ok && s.HTTPStatus() != 0 {
		details += fmt.Sprintf(" status=%d", s.HTTPStatus())
	}
	return details
}

// pyIsSpace is str.isspace for one character (shared pystr tables).
func pyIsSpace(r rune) bool { return pystr.IsSpace(r) }

// pyStrip is str.strip().
func pyStrip(s string) string { return pystr.Strip(s) }

// pyFields is str.split() without arguments.
func pyFields(s string) []string { return strings.FieldsFunc(s, pyIsSpace) }

// pyIsAlnum is str.isalnum() (false for the empty string).
func pyIsAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// isPyWord is a character of Python's Unicode \w.
func isPyWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

// pyLower is str.lower() (shared pystr tables).
func pyLower(s string) string { return pystr.Lower(s) }

// asciiLower lowers ASCII letters only (enough wherever the result is only
// compared with ASCII text: no other character lowers to ASCII).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// latin1 decodes bytes as ISO-8859-1 (how http.client decodes header values).
func latin1(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return string(runes)
}

// runePrefix is s[:n] on code points.
func runePrefix(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// pyInt is int(s) for a str (pystr.Int, base 10).
func pyInt(s string) (*big.Int, bool) {
	n, err := pystr.Int(s, 10)
	return n, err == nil
}

// pyOSError is str(OSError) for a failed file operation on path.
func pyOSError(err error, path string) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		text := errno.Error()
		if text != "" {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, jsonx.Repr(path))
	}
	return err.Error()
}

// osErrorClass is the OSError subclass Python raises for err.
func osErrorClass(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT:
			return "FileNotFoundError"
		case syscall.EACCES, syscall.EPERM:
			return "PermissionError"
		case syscall.EISDIR:
			return "IsADirectoryError"
		case syscall.ENOTDIR:
			return "NotADirectoryError"
		case syscall.EEXIST:
			return "FileExistsError"
		}
	}
	return "OSError"
}

// osError wraps a file-operation failure as Python's OSError.
func osError(err error, path string) *PyError {
	return &PyError{Class: osErrorClass(err), Message: pyOSError(err, path), Err: err}
}

// utf8DecodeError is the UnicodeDecodeError text of bytes.decode('utf-8')
// for invalid data ("" when data is valid UTF-8).
func utf8DecodeError(data []byte) string {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r != utf8.RuneError || size > 1 {
			i += size
			continue
		}
		n := maximalSubpart(data[i:])
		reason := "invalid start byte"
		if n > 1 || (data[i] >= 0xc2 && data[i] <= 0xf4) {
			if i+n >= len(data) {
				reason = "unexpected end of data"
			} else {
				reason = "invalid continuation byte"
			}
		}
		if n > 1 {
			return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", i, i+n-1, reason)
		}
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[i], i, reason)
	}
	return ""
}

// decodeUTF8Replace is bytes.decode('utf-8', errors='replace'): one U+FFFD
// per maximal subpart of an ill-formed sequence.
func decodeUTF8Replace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.Write(b[i : i+size])
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return out.String()
}

// maximalSubpart is the length of the ill-formed prefix CPython replaces
// with one U+FFFD (at least 1).
func maximalSubpart(b []byte) int {
	c := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xbf)
	switch {
	case c >= 0xc2 && c <= 0xdf:
		need = 1
	case c == 0xe0:
		need, lo = 2, 0xa0
	case c >= 0xe1 && c <= 0xec, c == 0xee, c == 0xef:
		need = 2
	case c == 0xed:
		need, hi = 2, 0x9f
	case c == 0xf0:
		need, lo = 3, 0x90
	case c >= 0xf1 && c <= 0xf3:
		need = 3
	case c == 0xf4:
		need, hi = 3, 0x8f
	default:
		return 1
	}
	n := 1
	for k := 0; k < need && n < len(b); k++ {
		x := b[n]
		if k == 0 {
			if x < lo || x > hi {
				return n
			}
		} else if x < 0x80 || x > 0xbf {
			return n
		}
		n++
	}
	return n
}

// isoFormat is datetime.isoformat() of an aware UTC datetime: microseconds
// only when not zero, "+00:00" suffix.
func isoFormat(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		base += fmt.Sprintf(".%06d", us)
	}
	return base + "+00:00"
}

// pyTime is time.time() for t: float seconds as _PyTime_AsSecondsDouble
// computes them from nanoseconds.
func pyTime(t time.Time) float64 {
	ns := t.UnixNano()
	if ns%1e9 == 0 {
		return float64(ns / 1e9)
	}
	return float64(ns) / 1e9
}

// typeName is type(v).__name__ of a JSON value.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case jsonx.Number:
		if v.(jsonx.Number).IsInt() {
			return "int"
		}
		return "float"
	case *jsonx.Object:
		return "dict"
	case []any:
		return "list"
	}
	return "object"
}

func attributeError(v any, name string) *PyError {
	return pyErr("AttributeError", fmt.Sprintf("'%s' object has no attribute '%s'", typeName(v), name))
}
