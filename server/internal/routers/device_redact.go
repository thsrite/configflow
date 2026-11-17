package routers

import (
	"net/url"
	"strings"
)

// Device response text can echo credentials (for example inside a console
// error). Keep row data intact for ownership checks and scrub only diagnostics.
func (d *RouterOS) safeDeviceText(text string) string {
	for _, secret := range []string{d.password} {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, form, "[REDACTED]")
		}
	}
	return text
}
