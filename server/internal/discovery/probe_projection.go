package discovery

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
)

// Clone detaches a probe from the read-only file snapshot.
func (p Probe) Clone() Probe {
	if p.Target != nil {
		p.Target = new(*p.Target)
	}
	p.Reasons = slices.Clone(p.Reasons)
	p.Direct = p.Direct.clone()
	p.Proxy = p.Proxy.clone()
	return p
}
func (s *PathStats) clone() *PathStats {
	if s == nil {
		return nil
	}
	v := *s
	v.Delays = slices.Clone(v.Delays)
	v.Errors = maps.Clone(v.Errors)
	return &v
}

func projectProbeRecord(host, checkedAt string, entry check.TimedRecord) (Probe, error) {
	p := Probe{CheckedAt: checkedAt}
	var firstErr error
	for key, raw := range entry.All() {
		var dst any
		switch key {
		case "host":
			dst = &p.Host
		case "url":
			dst = &p.URL
		case "verdict":
			dst = &p.Verdict
		case "target":
			dst = &p.Target
		case "confidence":
			dst = &p.Confidence
		case "reasons":
			dst = &p.Reasons
		case "direct":
			dst = &p.Direct
		case "proxy":
			dst = &p.Proxy
		case "proxy_path":
			dst = &p.ProxyPath
		default:
			continue
		}
		if err := jsondecode.Unmarshal(raw, dst); err != nil && firstErr == nil {
			var mismatch *json.UnmarshalTypeError
			if errors.As(err, &mismatch) {
				path := host + "." + key
				if mismatch.Field != "" {
					path += "." + mismatch.Field
				}
				mismatch.Struct, mismatch.Field = "", path
			}
			firstErr = err
		}
	}
	return p, firstErr
}
