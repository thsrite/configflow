package substore

import (
	"context"
	"errors"
	"io"
)

// Every wire body and every decoded Content-Encoding layer has this budget.
// Checking only Content-Length or the compressed bytes permits decompression bombs.
const maxResponseBytes = 64 << 20

// ErrResponseTooLarge rejects an HTTP body or decoded layer above 64 MiB.
var ErrResponseTooLarge = errors.New(msgResponseTooLarge)

func readResponseBody(ctx context.Context, reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maxResponseBytes+1))
	if len(data) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return data, err
}
