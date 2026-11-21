package agents

// HTTP wire helpers shared by Agent remote operations.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"configflow/internal/jsonresp"
	"golang.org/x/text/encoding/charmap"
)

// ErrHeaderEncoding rejects a header outside the Agent's Latin-1 wire contract.
var ErrHeaderEncoding = errors.New(msgAgentHeaderEncoding)

// EncodeHeaderLatin1 uses the native ISO-8859-1 codec. Invalid values are
// rejected without repeating credentials or interpreter exception details.
func EncodeHeaderLatin1(value string) (string, error) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrHeaderEncoding, err)
	}
	return encoded, nil
}

// jsonBody encodes an Agent request body with its Content-Type. Values that
// cannot be encoded (NaN, infinities) are refused before anything is sent,
// as a *TransportError.
func jsonBody(value any) (http.Header, []byte, error) {
	body, err := jsonresp.Encode(value)
	if err != nil {
		return nil, nil, &TransportError{Err: errors.New(msgInvalidDevicePayload)}
	}
	return http.Header{"Content-Type": {"application/json"}}, body, nil
}

// quote encodes Agent query values while retaining path separators.
func quote(value string) string {
	plus := url.QueryEscape(value)
	// quote_plus encodes "/" and " "; quote keeps "/" and writes %20.
	out := make([]byte, 0, len(plus))
	for i := 0; i < len(plus); i++ {
		switch {
		case plus[i] == '+':
			out = append(out, "%20"...)
		case plus[i] == '%' && i+2 < len(plus) && plus[i+1] == '2' && (plus[i+2] == 'F' || plus[i+2] == 'f'):
			out = append(out, '/')
			i += 2
		default:
			out = append(out, plus[i])
		}
	}
	return string(out)
}

// RemoteError carries a sanitized user-facing Agent refusal.
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }
