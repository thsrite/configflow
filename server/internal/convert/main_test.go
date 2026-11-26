package convert

import (
	"os"
	"testing"

	"configflow/internal/logx"
)

// TestMain keeps the converter's INFO lines out of the test output.
func TestMain(m *testing.M) {
	root, err := logx.Setup(logx.Config{Level: logx.Critical})
	if err != nil {
		panic(err)
	}
	code := m.Run()
	root.Close()
	os.Exit(code)
}
