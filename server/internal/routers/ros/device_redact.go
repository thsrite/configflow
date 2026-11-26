package ros

import "configflow/internal/redact"

// safeDeviceText hides device text that echoes the router password, in any
// URL-encoded form, using the shared redaction rules. Usernames are not
// secrets (the router form shows them) and often equal words of the fixed
// diagnostics, such as a Client user named "api" next to "rest-api"; they
// stay visible. Row data is never redacted, so ownership checks stay exact.
func (d *Client) SafeDeviceText(text string) string {
	if d.password == "" {
		return text
	}
	if redact.ContainsEncodedSecret(text, redact.NewSecrets([]string{d.password})) {
		return redact.Redacted
	}
	return text
}
