package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"configflow/internal/jsonresp"
)

// The JWT code is a port of the parts of PyJWT 2.8.0 the backend uses:
// jwt.encode(payload, key, algorithm="HS256") and
// jwt.decode(token, key, algorithms=["HS256"]) with default options. Error
// texts are PyJWT's, because require_auth shows them to the user.

// TokenError is a PyJWT exception. Expired distinguishes
// ExpiredSignatureError from the other InvalidTokenError subclasses.
type TokenError struct {
	Expired bool
	Message string
}

func (e *TokenError) Error() string { return e.Message }

func decodeError(message string) error { return &TokenError{Message: message} }

// EncodeHS256 is jwt.encode(claims, key, algorithm="HS256"): header
// {"alg":"HS256","typ":"JWT"} (sorted), the claims as compact UTF-8 JSON with
// native encoding, unpadded base64url segments, HMAC-SHA256 signature.
func EncodeHS256(claims any, key string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := jsonresp.Encode(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64(header) + "." + b64(string(payload))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// DecodeHS256 is jwt.decode(token, key, algorithms=["HS256"]) with leeway 0
// at time now. It returns the claims or a *TokenError (Expired for an expired
// token).
func DecodeHS256(token, key string, now time.Time) (*Claims, error) {
	signingInput, cryptoSegment, ok := cutLast(token, ".")
	if !ok {
		return nil, decodeError("Not enough segments")
	}
	headerSegment, payloadSegment, ok := strings.Cut(signingInput, ".")
	if !ok {
		return nil, decodeError("Not enough segments")
	}
	headerData, err := base64urlDecode(headerSegment)
	if err != nil {
		return nil, decodeError("Invalid header padding")
	}
	if bytes.HasPrefix(bytes.TrimSpace(headerData), []byte("[")) {
		return nil, decodeError("Invalid header string: must be a json object")
	}
	var header map[string]json.RawMessage
	err = json.Unmarshal(headerData, &header)
	if err != nil {
		return nil, decodeError("Invalid header string: " + err.Error())
	}
	if header == nil {
		return nil, decodeError("Invalid header string: must be a json object")
	}
	payloadData, err := base64urlDecode(payloadSegment)
	if err != nil {
		return nil, decodeError("Invalid payload padding")
	}
	signature, err := base64urlDecode(cryptoSegment)
	if err != nil {
		return nil, decodeError("Invalid crypto padding")
	}
	alg, present := header["alg"]
	if !present {
		return nil, decodeError("Algorithm not specified")
	}
	if claimString(alg) != "HS256" {
		return nil, decodeError("The specified alg value is not allowed")
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingInput))
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, decodeError("Signature verification failed")
	}
	if bytes.HasPrefix(bytes.TrimSpace(payloadData), []byte("[")) {
		return nil, decodeError("Invalid payload string: must be a json object")
	}
	var claims *Claims
	err = json.Unmarshal(payloadData, &claims)
	if err != nil {
		return nil, decodeError("Invalid payload string: " + err.Error())
	}
	if claims == nil {
		return nil, decodeError("Invalid payload string: must be a json object")
	}
	if err := validateClaims(claims, now); err != nil {
		return nil, err
	}
	return claims, nil
}

func cutLast(s, sep string) (before, after string, ok bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// validateClaims checks iat, nbf and exp (RFC 7519 NumericDate: a JSON
// number of seconds, fractions allowed) with leeway 0, then refuses any
// audience. A claim that is not a JSON number makes the token invalid.
func validateClaims(claims *Claims, now time.Time) error {
	nowSeconds := float64(now.UnixNano()) / 1e9
	if value := claims.IssuedAt; value != nil {
		iat, ok := numericDate(value)
		if !ok {
			return decodeError("Issued At claim (iat) must be a number.")
		}
		if iat > nowSeconds {
			return decodeError("The token is not yet valid (iat)")
		}
	}
	if value := claims.NotBefore; value != nil {
		nbf, ok := numericDate(value)
		if !ok {
			return decodeError("Not Before claim (nbf) must be a number.")
		}
		if nbf > nowSeconds {
			return decodeError("The token is not yet valid (nbf)")
		}
	}
	if value := claims.Expires; value != nil {
		exp, ok := numericDate(value)
		if !ok {
			return decodeError("Expiration Time claim (exp) must be a number.")
		}
		if exp <= nowSeconds {
			return &TokenError{Expired: true, Message: "Signature has expired"}
		}
	}
	if audiencePresent(claims.Audience) {
		return decodeError("Invalid audience")
	}
	return nil
}

// numericDate reads a NumericDate claim: a JSON number (not a string) that
// fits a float64.
func numericDate(raw json.RawMessage) (float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' {
		return 0, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

// base64urlDecode accepts URL-safe JWT segments, with optional final padding.
// Signatures are always verified over the original received segment bytes.
func base64urlDecode(segment string) ([]byte, error) {
	for _, ch := range segment {
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '=') {
			return nil, errors.New("invalid base64url character")
		}
	}
	if strings.Contains(segment, "=") {
		return base64.URLEncoding.DecodeString(segment)
	}
	return base64.RawURLEncoding.DecodeString(segment)
}

func audiencePresent(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}
