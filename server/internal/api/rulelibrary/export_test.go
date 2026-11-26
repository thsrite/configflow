package rulelibrary

import (
	"configflow/internal/httpx"
	"context"
	"os"
)

func replaceFile(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// RegisterWithOptions installs the library routes with an injected transport.
func RegisterWithOptions(r *httpx.Router, options Options) {
	h := New(Deps{Repo: options.Repo, Client: options.Client, Refresh: options.Refresh})
	h.service.replaceCache = options.replaceCache
	h.Register(r)
}
