package settings

import (
	"encoding/json"
	"errors"
)

// settingsRequest retains field presence and wire types for the existing
// validators. RawMessage is needed for port, retention and permissive settings
// values: decoding them as strings or floats would change validation outcomes.
type settingsRequest struct {
	Mode           json.RawMessage `json:"mode,omitempty"`
	Port           json.RawMessage `json:"port,omitempty"`
	RuleFetchProxy json.RawMessage `json:"rule_fetch_proxy,omitempty"`
	NewDomain      json.RawMessage `json:"new_domain,omitempty"`
	Token          json.RawMessage `json:"token,omitempty"`
	Generate       json.RawMessage `json:"generate,omitempty"`
	Enabled        json.RawMessage `json:"enabled,omitempty"`
	WebDAVURL      json.RawMessage `json:"webdav_url,omitempty"`
	WebDAVUsername json.RawMessage `json:"webdav_username,omitempty"`
	WebDAVPassword json.RawMessage `json:"webdav_password,omitempty"`
	WebDAVPath     json.RawMessage `json:"webdav_path,omitempty"`
	AutoBackup     json.RawMessage `json:"auto_backup,omitempty"`
	KeepLatest     json.RawMessage `json:"keep_latest,omitempty"`
	KeepDaily      json.RawMessage `json:"keep_daily,omitempty"`
	nonempty       bool
}

// UnmarshalJSON requires an object and uses exact JSON member names. Unknown
// members are ignored, except that an unknown-only body is still nonempty.
func (r *settingsRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("settings request must be an object")
	}
	*r = settingsRequest{nonempty: len(fields) > 0,
		Mode: fields["mode"], Port: fields["port"], RuleFetchProxy: fields["rule_fetch_proxy"], NewDomain: fields["new_domain"],
		Token: fields["token"], Generate: fields["generate"], Enabled: fields["enabled"],
		WebDAVURL: fields["webdav_url"], WebDAVUsername: fields["webdav_username"],
		WebDAVPassword: fields["webdav_password"], WebDAVPath: fields["webdav_path"],
		AutoBackup: fields["auto_backup"], KeepLatest: fields["keep_latest"], KeepDaily: fields["keep_daily"],
	}
	return nil
}
