package settings

import (
	"errors"
	"fmt"
	"net/url"

	"configflow/internal/httpx"
	"configflow/internal/webdav"
)

// backupEditorURL keeps the editable address without returning URL credentials.
func backupEditorURL(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Opaque != "" {
		return ""
	}
	u.User = nil
	return u.String()
}

func (h *Handler) testBackup(c *httpx.Ctx) (*httpx.Response, error) {
	settings, bad, err := h.connectionSettings(c, false)
	if bad != nil || err != nil {
		return bad, err
	}
	connected, err := h.testBackupConnection(c.Context(), settings)
	if err != nil {
		var directory *webdav.DirectoryError
		if errors.Is(err, webdav.ErrIncompleteSettings) || errors.Is(err, webdav.ErrUnsafePath) || errors.As(err, &directory) {
			return failure(400, err.Error()), nil
		}
		detail := webdav.DescribeError(err)
		appLogger.Errorf(msgBackupConnectLog, detail)
		return failure(500, fmt.Sprintf(msgBackupConnectError, detail)), nil
	}
	if !connected {
		return failure(400, msgBackupConnectFailed), nil
	}
	return httpx.Message(200, true, msgBackupConnectOK), nil
}
func (h *Handler) backupNow(c *httpx.Ctx) (*httpx.Response, error) {
	settings, bad, err := h.connectionSettings(c, true)
	if bad != nil || err != nil {
		return bad, err
	}
	result, err := h.runManualBackup(c.Context(), settings)
	if err == nil {
		cleaned := ""
		if result.Removed > 0 {
			cleaned = fmt.Sprintf(msgBackupCleaned, result.Removed)
		}
		return httpx.Message(200, true, fmt.Sprintf(msgBackupUploaded, result.Filename, cleaned)), nil
	}

	var directory *webdav.DirectoryError
	if errors.Is(err, webdav.ErrIncompleteSettings) || errors.Is(err, webdav.ErrUnsafePath) || errors.As(err, &directory) {
		return failure(400, err.Error()), nil
	}
	detail := webdav.DescribeError(err)
	appLogger.Errorf(msgBackupManualLog, detail)
	return failure(500, fmt.Sprintf(msgBackupFailed, detail)), nil
}
