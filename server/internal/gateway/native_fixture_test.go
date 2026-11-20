package gateway

import (
	"configflow/internal/model"
	"encoding/json"
)

// NewFixtureConfig captures existing JSON fixtures through the native model.
func NewFixtureConfig(value any) *Config { return NewConfig(NewFixtureModel(value)) }

// NewFixtureModel captures an existing JSON fixture with the shared model codec.
func NewFixtureModel(value any) *model.Config {
	raw, _ := json.Marshal(value)
	config, _ := model.DecodeConfig(raw)
	return config
}
