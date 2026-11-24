package substore

import (
	"errors"
)

// StatusOf is getattr(getattr(exc, 'response', None), 'status_code', None)
// (0 = none).
func StatusOf(err error) int {
	var request *RequestError
	if errors.As(err, &request) {
		return request.Status
	}
	return 0
}
