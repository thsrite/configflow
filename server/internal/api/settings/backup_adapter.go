package settings

import (
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/model"
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
	if string(data.WebDAVPassword) == `"******"` {
		data.WebDAVPassword, _ = json.Marshal(saved.Backup.WebDAVPassword)
	}
	if inheritRetention {
		if len(data.KeepLatest) == 0 {
			data.KeepLatest, _ = json.Marshal(saved.Backup.KeepLatest)
		}
		if len(data.KeepDaily) == 0 {
			data.KeepDaily, _ = json.Marshal(saved.Backup.KeepDaily)
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
func retentionSettings(settings *model.Backup) (any, any) {
	raw, _ := json.Marshal(settings)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	return webdav.RetentionValue(fields["keep_latest"], webdav.DefaultKeepLatest, webdav.MaxKeepLatest), webdav.RetentionValue(fields["keep_daily"], webdav.DefaultKeepDaily, webdav.MaxKeepDaily)
}
