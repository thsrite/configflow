//go:build configflow_proddiff

package lifecycle

import (
	"os"
	"strings"
)

func defaultMode() Mode { return ModeManual }
func reportReady(r *Runtime) {
	path := os.Getenv("PRODDIFF_ROUTES")
	if path == "" {
		return
	}
	var startups []string
	for _, s := range r.registry.snapshot().startups {
		startups = append(startups, s.Name)
	}
	for suffix, data := range map[string][]byte{".loops": []byte(strings.Join(r.Names(), "\n")), ".startups": []byte(strings.Join(startups, "\n"))} {
		if err := os.WriteFile(path+suffix, data, 0o600); err != nil {
			logger.Errorf(msgReadyFailed, err)
			return
		}
	}
	if err := os.WriteFile(path+".ready", nil, 0o600); err != nil {
		logger.Errorf(msgReadyFailed, err)
	}
}
