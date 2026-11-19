package trafficstore

import (
	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Store adapts legacy dynamic callers to NativeStore.
type Store struct {
	*NativeStore
	ReadJSON  func(string) *jsonx.Object
	WriteJSON func(string, *jsonx.Object) (*jsonstore.Signature, error)
}

func toObject(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out *jsonx.Object
	err = json.Unmarshal(raw, &out)
	return out, err
}

// New returns the compatibility facade. Native callers use NewNative.
func New(dir string) *Store {
	s := &Store{NativeStore: NewNative(dir)}
	s.ReadJSON = func(id string) *jsonx.Object { var out *jsonx.Object; s.files.Read(id, &out); return out }
	s.WriteJSON = func(id string, out *jsonx.Object) (*jsonstore.Signature, error) { return s.files.Write(id, out, true) }
	s.readJSON = func(id string) json.RawMessage {
		out := s.ReadJSON(id)
		if out == nil {
			return nil
		}
		raw, _ := json.Marshal(out)
		return raw
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

// CleanReport transcodes dynamic input to the native exact-key decoder.
func CleanReport(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	r, err := DecodeReport(raw)
	if err != nil || r == nil {
		return nil, err
	}
	return toObject(r)
}

// AddReport transcodes a cleaned report and native status response.
func (s *Store) AddReport(id string, report *jsonx.Object, at *time.Time) (*jsonx.Object, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	r, err := decodeCleanedReport(raw)
	if err != nil {
		return nil, err
	}
	status, err := s.Add(context.Background(), id, r, at)
	if err != nil {
		return nil, err
	}
	return toObject(status)
}

// Load returns a compatibility copy of the native document.
func (s *Store) Load(id string, at *time.Time) *jsonx.Object {
	out, _ := toObject(s.LoadDocument(id, at))
	return out
}

// LoadStatus returns a compatibility copy of native metadata.
func (s *Store) LoadStatus(id string) any {
	raw := s.StatusJSON(id)
	if len(raw) == 0 {
		return nil
	}
	value, _ := jsonx.Parse(raw)
	return value
}

// StatusOf returns object metadata for legacy discovery consumers.
func (s *Store) StatusOf(id string) *jsonx.Object {
	out, _ := s.LoadStatus(id).(*jsonx.Object)
	return out
}

// SplitRouteKey adapts the typed route decoder.
func SplitRouteKey(key string) (*jsonx.Object, bool) {
	r, ok := ParseRouteKey(key)
	if !ok {
		return nil, false
	}
	out, _ := toObject(r)
	return out, true
}

// CachedStatus returns an independent compatibility cache snapshot.
func CachedStatus(path string) *jsonx.Object {
	mu.Lock()
	defer mu.Unlock()
	if e, ok := statusIndex[path]; ok {
		out, _ := toObject(e.Value.(*cacheEntry).status)
		return out
	}
	return nil
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
