package substore

import (
	"io"
	"os"
	"testing"

	"configflow/internal/lockx"
	"configflow/internal/logx"
)

func TestMain(m *testing.M) {
	logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard})
	code := m.Run()
	if os.Getenv("CONFIGFLOW_SUBSTORE_ORDER_HELPER") != "1" && lockx.Violations() != 0 {
		logx.Get("backend.lockx").Errorf("unexpected Sub-Store lock violations: %d", lockx.Violations())
		code = 1
	}
	os.Exit(code)
}
