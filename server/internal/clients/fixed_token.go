package clients

import (
	"bytes"
	"compress/zlib"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var errFixedToken = errors.New("invalid fixed IP token")

const fixedTokenSalt = "client-fixed-ip-v1"

// FixedIPToken signs the existing itsdangerous URLSafeTimedSerializer protocol.
// The payload is native JSON; HMAC derivation, timestamp and compression remain
// wire compatible, so both implementations can verify the other's tokens.
type FixedIPToken struct {
	Secret string
	Now    func() time.Time
}

func (s FixedIPToken) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s FixedIPToken) signature(data string) string {
	key := sha1.Sum([]byte(fixedTokenSalt + "signer" + s.Secret))
	mac := hmac.New(sha1.New, key[:])
	_, _ = mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Sign produces a timestamped, optionally compressed preview token.
func (s FixedIPToken) Sign(payload json.RawMessage) (string, error) {
	if !json.Valid(payload) {
		return "", errFixedToken
	}
	data := []byte(payload)
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	prefix := ""
	if compressed.Len() < len(data)-1 {
		prefix = "."
		data = compressed.Bytes()
	}
	encoded := prefix + base64.RawURLEncoding.EncodeToString(data)
	var timestamp [8]byte
	binary.BigEndian.PutUint64(timestamp[:], uint64(s.now().Unix()))
	trimmed := bytes.TrimLeft(timestamp[:], "\x00")
	signed := encoded + "." + base64.RawURLEncoding.EncodeToString(trimmed)
	return signed + "." + s.signature(signed), nil
}

// Verify authenticates before decoding and refuses expired or future timestamps.
func (s FixedIPToken) Verify(token string) (json.RawMessage, error) {
	if token == "" || len(token) > 1000000 {
		return nil, errFixedToken
	}
	position := strings.LastIndexByte(token, '.')
	if position < 0 {
		return nil, errFixedToken
	}
	signed, signature := token[:position], token[position+1:]
	if !hmac.Equal([]byte(signature), []byte(s.signature(signed))) {
		return nil, errFixedToken
	}
	position = strings.LastIndexByte(signed, '.')
	if position < 0 {
		return nil, errFixedToken
	}
	timestamp, err := base64.RawURLEncoding.DecodeString(signed[position+1:])
	if err != nil || len(timestamp) > 8 {
		return nil, errFixedToken
	}
	var stamp [8]byte
	copy(stamp[8-len(timestamp):], timestamp)
	seconds := binary.BigEndian.Uint64(stamp[:])
	now := s.now().Unix()
	if now < 0 || seconds > uint64(now) || uint64(now)-seconds > 600 {
		return nil, errFixedToken
	}
	encoded := signed[:position]
	compressed := strings.HasPrefix(encoded, ".")
	encoded = strings.TrimPrefix(encoded, ".")
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errFixedToken
	}
	if compressed {
		reader, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, errFixedToken
		}
		data, err = io.ReadAll(io.LimitReader(reader, 8*1024*1024+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil || len(data) > 8*1024*1024 {
			return nil, errFixedToken
		}
	}
	if !json.Valid(data) {
		return nil, errFixedToken
	}
	return data, nil
}
