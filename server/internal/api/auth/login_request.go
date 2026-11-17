package auth

import (
	"encoding/json"

	"configflow/internal/httpx"
)

// loginRequest deliberately retains credential values before validation. Valid
// JSON fields of other types are incorrect credentials (401), not malformed DTOs.
type loginRequest struct{ Username, Password json.RawMessage }

func (r *loginRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return httpx.BadRequest("")
	}
	r.Username, r.Password = fields["username"], fields["password"]
	return nil
}

func (r loginRequest) credentials() (string, string) {
	var username, password string
	_ = json.Unmarshal(r.Username, &username)
	_ = json.Unmarshal(r.Password, &password)
	return username, password
}
