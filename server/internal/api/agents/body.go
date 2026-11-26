package agents

import (
	"bytes"
	"encoding/json"
	"io"

	"configflow/internal/httpx"
)

// readBoundJSON keeps the endpoint's limit/error envelope while using Bind's
// exact-key, native JSON decoding and byte-encoding policy.
func readBoundJSON(c *httpx.Ctx, limit int64, invalid string) (json.RawMessage, *httpx.Response) {
	if length, ok := c.ContentLength(); ok && length > limit {
		return nil, failure(413, "Request body too large")
	}
	var raw []byte
	var err error
	if stream := c.Stream(); stream != nil {
		raw, err = io.ReadAll(io.LimitReader(stream, limit+1))
	}
	if int64(len(raw)) > limit {
		return nil, failure(413, "Request body too large")
	}
	if err != nil {
		return nil, failure(400, invalid)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var body json.RawMessage
	if c.Bind(&body) != nil {
		return nil, failure(400, invalid)
	}
	return body, nil
}

// bindForce preserves endpoints that accept JSON without a JSON media type.
func bindForce(c *httpx.Ctx, dst any) error {
	previous := c.Request.Header.Values("Content-Type")
	c.Request.Header.Set("Content-Type", "application/json")
	defer func() {
		c.Request.Header.Del("Content-Type")
		for _, value := range previous {
			c.Request.Header.Add("Content-Type", value)
		}
	}()
	return c.Bind(dst)
}
