package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

// signRaw builds an HS256 token over a literal payload, so the test can use
// JSON spellings EncodeHS256 would never write.
func signRaw(t *testing.T, header, payload, key string) string {
	t.Helper()
	enc := base64.RawURLEncoding
	input := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(input))
	return input + "." + enc.EncodeToString(mac.Sum(nil))
}

// O18 C17 review: NumericDate decoding is never looser than before. Every
// non-number spelling, overflow and boundary is refused; fractions compare
// exactly; signature and alg checks still run first.
func TestNumericDateClaimsReview(t *testing.T) {
	const key = "synthetic-secret"
	now := time.Unix(1_800_000_000, 0)
	header := `{"alg":"HS256","typ":"JWT"}`
	tests := []struct {
		name, payload string
		ok, expired   bool
	}{
		{"valid", `{"exp":1800000100,"iat":1799999000,"nbf":1799999000}`, true, false},
		{"exp_equal_now", `{"exp":1800000000}`, false, true},
		{"exp_past_fraction", `{"exp":1799999999.999}`, false, true},
		{"exp_future_fraction", `{"exp":1800000000.5}`, true, false},
		{"exp_negative", `{"exp":-1}`, false, true},
		{"exp_overflow", `{"exp":1e400}`, false, false},
		{"exp_negative_overflow", `{"exp":-1e400}`, false, false},
		{"exp_huge_int", `{"exp":123456789012345678901234567890}`, true, false},
		{"exp_null", `{"exp":null}`, false, false},
		{"exp_true", `{"exp":true}`, false, false},
		{"exp_string", `{"exp":"1900000000"}`, false, false},
		{"exp_array", `{"exp":[1900000000]}`, false, false},
		{"exp_object", `{"exp":{}}`, false, false},
		{"iat_future", `{"iat":1800000001}`, false, false},
		{"iat_future_fraction", `{"iat":1800000000.5}`, false, false},
		{"iat_overflow", `{"iat":1e400}`, false, false},
		{"nbf_future", `{"nbf":1800000000.001}`, false, false},
		{"nbf_null", `{"nbf":null}`, false, false},
		{"nbf_overflow_negative", `{"nbf":-1e400}`, false, false},
		{"claims_absent", `{"sub":"admin"}`, true, false},
		{"exp_wrong_case_ignored", `{"EXP":1}`, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeHS256(signRaw(t, header, tt.payload, key), key, now)
			if tt.ok {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			te, isToken := err.(*TokenError)
			if !isToken {
				t.Fatalf("want *TokenError, got %T %v", err, err)
			}
			if te.Expired != tt.expired {
				t.Fatalf("expired %v, want %v (%s)", te.Expired, tt.expired, te.Message)
			}
		})
	}

	// The signature and alg are checked before any claim.
	for name, token := range map[string]string{
		"wrong_key":  signRaw(t, header, `{"exp":1900000000}`, "other"),
		"alg_none":   signRaw(t, `{"alg":"none"}`, `{"exp":1900000000}`, key),
		"alg_hs512":  signRaw(t, `{"alg":"HS512"}`, `{"exp":1900000000}`, key),
		"alg_number": signRaw(t, `{"alg":256}`, `{"exp":1900000000}`, key),
	} {
		if _, err := DecodeHS256(token, key, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
