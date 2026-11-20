package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"math/big"
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

// ClaimTypeError is the TypeError PyJWT lets escape when a numeric claim is
// not a number or a string (int(None), int([])...): not an InvalidTokenError,
// so Python's verify_token does not catch it and the request fails with 500.
type ClaimTypeError struct{ Message string }

func (e *ClaimTypeError) Error() string { return e.Message }

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
// at time now. It returns the claims, a *TokenError (Expired for
// ExpiredSignatureError), or a *ClaimTypeError.
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

// validateClaims is PyJWT 2.8.0 _validate_claims with default options and no
// audience or issuer: iat, nbf, exp in that order, then aud.
func validateClaims(claims *Claims, now time.Time) error {
	nowSeconds := float64(now.UnixNano()) / 1e9
	if value := claims.IssuedAt; value != nil {
		iat, err := claimInt(value)
		if err != nil {
			if errors.Is(err, errClaimValue) {
				return decodeError("Issued At claim (iat) must be an integer.")
			}
			return err
		}
		if compareFloat(iat, nowSeconds) > 0 {
			return decodeError("The token is not yet valid (iat)")
		}
	}
	if value := claims.NotBefore; value != nil {
		nbf, err := claimInt(value)
		if err != nil {
			if errors.Is(err, errClaimValue) {
				return decodeError("Not Before claim (nbf) must be an integer.")
			}
			return err
		}
		if compareFloat(nbf, nowSeconds) > 0 {
			return decodeError("The token is not yet valid (nbf)")
		}
	}
	if value := claims.Expires; value != nil {
		exp, err := claimInt(value)
		if err != nil {
			if errors.Is(err, errClaimValue) {
				return decodeError("Expiration Time claim (exp) must be an integer.")
			}
			return err
		}
		if compareFloat(exp, nowSeconds) <= 0 {
			return &TokenError{Expired: true, Message: "Signature has expired"}
		}
	}
	if audiencePresent(claims.Audience) {
		return decodeError("Invalid audience")
	}
	return nil
}

var errClaimValue = errors.New("ValueError")

// claimInt is Python's int(value) for a decoded claim: ValueError for text
// that is not an integer and for NaN, TypeError (and OverflowError for
// infinity) for the rest.
func claimInt(raw json.RawMessage) (*big.Int, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, errClaimValue
	}
	switch v := value.(type) {
	case bool:
		if v {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case json.Number:
		if !strings.ContainsAny(string(v), ".eE") {
			n, ok := new(big.Int).SetString(v.String(), 10)
			if ok {
				return n, nil
			}
		}
		f, _ := v.Float64()
		if math.IsNaN(f) {
			return nil, errClaimValue
		}
		if math.IsInf(f, 0) {
			return nil, &ClaimTypeError{Message: "cannot convert float infinity to integer"}
		}
		n, _ := new(big.Float).SetFloat64(math.Trunc(f)).Int(nil)
		return n, nil
	case string:
		n, ok := parseClaimInt(v)
		if !ok {
			return nil, errClaimValue
		}
		return n, nil
	}
	return nil, &ClaimTypeError{Message: "numeric JWT claim must be an integer or numeric string"}
}

func parseClaimInt(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	return new(big.Int).SetString(s, 10)
}

func compareFloat(n *big.Int, f float64) int {
	return new(big.Float).SetInt(n).Cmp(big.NewFloat(f))
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
