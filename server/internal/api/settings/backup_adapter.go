package settings

import (
	"context"
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/webdav"
)

func connectionSettings(c *httpx.Ctx, inheritRetention bool) (webdav.Settings, *httpx.Response, error) {
	data, bad := backupRequestData(c)
	if bad != nil {
		return webdav.Settings{}, bad, nil
	}
	data = data.DeepCopy()
	saved, err := repo.GetSystemConfig(context.WithoutCancel(c.Context()))
	if err != nil {
		return webdav.Settings{}, nil, err
	}
	backup := saved.GetObject("backup")
	if data.GetString("webdav_password") == "******" {
		data.Set("webdav_password", backup.GetString("webdav_password"))
	}
	if inheritRetention {
		for _, key := range retentionKeys {
			if !data.Has(key) {
				if value, present := backup.Get(key); present {
					data.Set(key, value)
				}
			}
		}
	}
	raw, err := jsonx.Compact(data)
	if err != nil {
		return webdav.Settings{}, nil, err
	}
	var settings webdav.Settings
	err = json.Unmarshal(raw, &settings)
	return settings, nil, err
}

func retentionSettings(settings *jsonx.Object) (any, any) {
	latest, _ := jsonx.Compact(settings.Value("keep_latest"))
	daily, _ := jsonx.Compact(settings.Value("keep_daily"))
	return webdav.RetentionValue(latest, webdav.DefaultKeepLatest, webdav.MaxKeepLatest), webdav.RetentionValue(daily, webdav.DefaultKeepDaily, webdav.MaxKeepDaily)
}
