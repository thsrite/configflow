package rulefetch

import (
	"errors"
	"io"
	"sync"
)

// MaxDecodedBytes bounds every rule response after gzip/deflate decoding,
// independently of Content-Length and the caller's smaller deployment limit.
const MaxDecodedBytes int64 = 64 << 20

type decodedLimitBody struct {
	source    io.ReadCloser
	remaining int64
	closed    sync.Once
	closeErr  error
}

func boundDecodedBody(body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return nil
	}
	if _, ok := body.(*decodedLimitBody); ok {
		return body
	}
	return &decodedLimitBody{source: body, remaining: MaxDecodedBytes}
}

func (b *decodedLimitBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining < 0 {
		return 0, ErrSizeLimit
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.source.Read(p)
	if int64(n) > b.remaining {
		allowed := int(b.remaining)
		b.remaining = -1
		_ = b.Close()
		return allowed, ErrSizeLimit
	}
	if errors.Is(err, ErrSizeLimit) {
		b.remaining = -1
		_ = b.Close()
		return n, err
	}
	b.remaining -= int64(n)
	return n, err
}

func (b *decodedLimitBody) Close() error {
	b.closed.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}
