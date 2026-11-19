package trafficstore

import (
	"configflow/internal/jsondecode"
	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

// Store adapts legacy dynamic callers to NativeStore.
type Store struct {
	*NativeStore
	ReadJSON  func(string) *jsonx.Object
	WriteJSON func(string, *jsonx.Object) (*jsonstore.Signature, error)
}

// New returns the compatibility facade. Native callers use NewNative.
func New(dir string) *Store {
	s := &Store{NativeStore: NewNative(dir)}
	nativeRead := s.readJSON
	s.ReadJSON = func(id string) *jsonx.Object {
		raw, err := nativeRead(id)
		if err != nil || raw == nil || !utf8.Valid(raw) {
			return nil
		}
		var out *jsonx.Object
		if jsondecode.Unmarshal(raw, &out) != nil {
			return nil
		}
		return out
	}
	s.WriteJSON = func(id string, out *jsonx.Object) (*jsonstore.Signature, error) { return s.files.Write(id, out, true) }
	s.readJSON = func(id string) (json.RawMessage, error) {
		out := s.ReadJSON(id)
		if out == nil {
			return nativeRead(id)
		}
		return json.Marshal(out)
	}
	s.writeJSON = func(ctx context.Context, id string, raw json.RawMessage) (*jsonstore.Signature, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var out *jsonx.Object
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return s.WriteJSON(id, out)
	}
	return s
}

// Get returns the compatibility store for the active repository.
func Get(ctx context.Context) (*Store, error) {
	native, err := GetNative(ctx)
	if err != nil {
		return nil, err
	}
	return New(native.files.DataDir), nil
}

// StatusOf returns object metadata for legacy discovery consumers.
func (s *Store) StatusOf(id string) *jsonx.Object {
	raw := s.StatusJSON(id)
	if len(raw) == 0 {
		return nil
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	out, _ := value.(*jsonx.Object)
	return out
}

// Delete delegates removal and keeps the former error wrapper.
func (s *Store) Delete(id string) error {
	err := s.DeleteContext(context.Background(), id)
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return &OSError{Errno: errno, Path: s.Path(id)}
	}
	return err
}

// OSError retains the compatibility error shape for former callers.
type OSError struct {
	Errno syscall.Errno
	Path  string
}

func (e *OSError) Error() string {
	text := e.Errno.Error()
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return "[Errno " + strconv.Itoa(int(e.Errno)) + "] " + text + ": " + jsonx.Repr(e.Path)
}
func (e *OSError) Unwrap() error { return e.Errno }
