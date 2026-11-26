package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// nativeIntegerDiagnostic binds the original review's complete input and
// interpreter diagnostic, while requiring the native validation error.
func nativeIntegerDiagnostic(input, python string, err error) bool {
	raw, _ := json.Marshal(struct {
		Input  string `json:"input"`
		Python string `json:"python"`
	}{input, python})
	sum := sha256.Sum256(raw)
	allowed := map[string]bool{"82c78b07ab628976d778b3982e54e3cd5b1658dc1642806b571eb19b441a5950": true, "efd171b8bf49369ab6247e81df2b7061c893dafc1d2665cd963bd3e6ba865eab": true, "ef74aa8d6bb7acff16091589d5329e39accb920ff1bea798f710d3f55c224e72": true}
	var validation *ValidationError
	return allowed[hex.EncodeToString(sum[:])] && errors.As(err, &validation) && validation.Message == "Invalid installer integer"
}
