package substore

import (
	"io"
	"os"
	"testing"

	"configflow/internal/lockx"
	"configflow/internal/logx"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == fakeProcessArg {
		os.Exit(runFakeProcess(os.Args[2], os.Args[3]))
	}
	logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard})
	code := m.Run()
	if os.Getenv("CONFIGFLOW_SUBSTORE_ORDER_HELPER") != "1" && lockx.Violations() != 0 {
		logx.Get("backend.lockx").Errorf("unexpected Sub-Store lock violations: %d", lockx.Violations())
		code = 1
	}
	os.Exit(code)
}
