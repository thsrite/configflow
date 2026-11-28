package repo

import (
	"context"
	"os"
	"sync"

	"configflow/internal/jsonstore"
)

// SharedFileView retains one immutable projection of a signed shared file.
// Use a separate view for each decoder. Read resolves the path and acquires
// the same file lease as ReadSharedBytes, including on a hit.
type SharedFileView[T any] struct {
	mu    sync.Mutex
	path  string
	sig   jsonstore.Signature
	value T
}

// Read returns a borrowed value. Failed reads and concurrent replacements
// never populate the view.
func (v *SharedFileView[T]) Read(ctx context.Context, r *Repository, relative string, decode func([]byte) (T, error)) (value T, err error) {
	path, err := r.SharedPath(relative)
	if err != nil {
		return value, err
	}
	err = withFile(ctx, path, func() error {
		before, statErr := jsonstore.FileSignature(path)
		v.mu.Lock()
		defer v.mu.Unlock()
		if statErr == nil && v.path == path && v.sig == before {
			value = v.value
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		value, err = decode(data)
		if err == nil && statErr == nil {
			if after, e := jsonstore.FileSignature(path); e == nil && after == before {
				v.path, v.sig, v.value = path, before, value
			}
		}
		return err
	})
	return value, err
}
