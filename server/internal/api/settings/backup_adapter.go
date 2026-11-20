package settings

import (
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/webdav"
)

func connectionSettings(c *httpx.Ctx, inheritRetention bool) (webdav.Settings, *httpx.Response, error) {
	data, bad := backupRequestData(c)
	if bad != nil {
		return webdav.Settings{}, bad, nil
	}
	saved, err := loadSettings(c.Context())
	if err != nil {
		return webdav.Settings{}, nil, err
	}
	savedRaw, err := json.Marshal(saved.Backup)
	if err != nil {
		return webdav.Settings{}, nil, err
	}
	var stored map[string]json.RawMessage
	if err = json.Unmarshal(savedRaw, &stored); err != nil {
		return webdav.Settings{}, nil, err
	}
	if string(data.WebDAVPassword) == `"******"` {
		var password string
		_ = json.Unmarshal(stored["webdav_password"], &password)
		data.WebDAVPassword, _ = json.Marshal(password)
	}
	if inheritRetention {
		if len(data.KeepLatest) == 0 {
			data.KeepLatest = stored["keep_latest"]
		}
		if len(data.KeepDaily) == 0 {
			data.KeepDaily = stored["keep_daily"]
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return webdav.Settings{}, nil, err
	}
	var settings webdav.Settings
	err = json.Unmarshal(raw, &settings)
	return settings, nil, err
}
