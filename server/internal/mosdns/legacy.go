package mosdns

// Legacy entry points are transcoders for callers owned by parallel N8 groups
// (W6 deploy/adapter.go). Generation, selection and parsing use the typed
// implementations exclusively.
import (
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func legacyConfig(config *jsonx.Object) (*model.Config, error) {
	projection := jsonx.NewObject()
	if config != nil {
		for _, key := range []string{"mosdns", "rule_configs", "rules", "rule_sets", "system_config", "profile_id", "rule_library"} {
			if value, ok := config.Get(key); ok {
				projection.Set(key, value)
			}
		}
	}
	data, err := jsonx.Compact(projection)
	if err != nil {
		return nil, err
	}
	return ConfigFromJSON(data)
}

func legacyList(value any) ([]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	out, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	list, ok := out.([]any)
	if !ok {
		return nil, fmt.Errorf("mosdns: expected array")
	}
	return list, nil
}

// GenerateMosDNSConfig transcodes an old configuration into Generate.
func GenerateMosDNSConfig(config *jsonx.Object, baseURL string) (string, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return "", err
	}
	return Generate(native, baseURL)
}

// GetMosDNSRulesetDownloads transcodes Downloads for existing generator owners.
func GetMosDNSRulesetDownloads(config *jsonx.Object, baseURL string) ([]any, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	out, err := Downloads(native, baseURL)
	if err != nil {
		return nil, err
	}
	return legacyList(out)
}

// GetMosDNSCustomFiles transcodes CustomFiles for existing generator owners.
func GetMosDNSCustomFiles(config *jsonx.Object) ([]any, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	out, err := CustomFiles(native)
	if err != nil {
		return nil, err
	}
	return legacyList(out)
}
