package substore

import (
	"io"
	"os"
	"testing"

	"configflow/internal/logx"
)

func TestMain(m *testing.M) {
	logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard})
	os.Exit(m.Run())
}
