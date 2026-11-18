package rulefetch

import (
	"configflow/internal/jsonx"
	"encoding/json"
)

// KnownService adapts the old response boundary to LookupService.
func KnownService(host string) *jsonx.Object {
	result := LookupService(host)
	if result == nil {
		return nil
	}
	return legacyObject(result)
}

// PageIdentity adapts archived callers; Agent protocol data is a typed record.
func PageIdentity(result *jsonx.Object, host string, checkedAt any) *jsonx.Object {
	var probe PageProbe
	if result != nil {
		// Decode each declared member independently: an invalid marker or error
		// cannot discard the Agent's unrelated status/URL fields.
		status, _ := jsonx.Compact(result.Value("status"))
		probe.Status = json.RawMessage(status)
		probe.Error, _ = result.Value("error").(string)
		probe.FinalURL, _ = result.Value("final_url").(string)
		_ = decodeLegacy(result.GetObject("markers"), &probe.Markers)
	}
	at, _ := checkedAt.(string)
	return legacyObject(IdentifyPage(probe, host, at))
}
