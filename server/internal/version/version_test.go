package version

import (
	"testing"

	"encoding/json"
)

func TestInfo(t *testing.T) {
	body, _ := json.Marshal(Info())
	if string(body) != `{"version":"dev"}` {
		t.Errorf("got %s", body)
	}
	Version = "1.2.3"
	defer func() { Version = "dev" }()
	if Info().Version != "1.2.3" {
		t.Error("Info does not follow Version")
	}
}
