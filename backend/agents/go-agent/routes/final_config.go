package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// renderFinalConfig only reads target settings. It never stages, activates,
// downloads, or writes a configuration. Native validation still runs on push.
func renderFinalConfig(cfg *Config, content string) (string, error) {
	if cfg.ServiceType != "mihomo" {
		return content, nil
	}
	content, err := adaptNativeAIODNS(cfg, content)
	if err != nil {
		return "", err
	}
	content, err = adaptGatewayDNS(cfg, content)
	if err != nil {
		return "", err
	}
	var doc map[string]interface{}
	if err = yaml.Unmarshal([]byte(content), &doc); err != nil || len(doc) == 0 {
		return "", fmt.Errorf("configuration must be a nonempty YAML mapping")
	}
	for _, section := range []string{"proxy-providers", "rule-providers"} {
		providers, _ := doc[section].(map[string]interface{})
		for _, value := range providers {
			provider, _ := value.(map[string]interface{})
			if raw, ok := provider["path"].(string); ok && raw != "" {
				rel := filepath.Clean(raw)
				if filepath.IsAbs(raw) {
					rel, err = filepath.Rel(filepath.Dir(cfg.ConfigPath), raw)
					if err != nil {
						return "", err
					}
				}
				rel = filepath.ToSlash(rel)
				if !safeRelative(rel) {
					return "", fmt.Errorf("configuration file reference escapes service directory")
				}
				provider["path"] = rel
			}
		}
	}
	data, err := yaml.Marshal(doc)
	return string(data), err
}

func FinalConfigPreviewHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
		var input struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Content == "" {
			JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "Invalid configuration preview"})
			return
		}
		content, err := renderFinalConfig(cfg, input.Content)
		if err != nil {
			JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "content": content})
	}
}
