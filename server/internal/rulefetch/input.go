package rulefetch

import (
	"configflow/internal/model"
	"context"
	"time"
)

// SourceConfig is the consumed rule/download projection of a config snapshot.
type SourceConfig struct {
	cacheLibrary func() []model.RuleLibraryItem
	Settings     model.SystemConfig
	Library      []model.RuleLibraryItem
	Rules        []model.RuleConfig
	RuleSets     []model.RuleConfig
}

func validateBytes(content Content, rule model.RuleConfig, config *SourceConfig, format string) error {
	var library []model.RuleLibraryItem
	if config != nil {
		library = config.Library
	}
	return ValidateRemote(content, rule, library, format)
}
func downloadContent(ctx context.Context, client *Client, url string, config *SourceConfig, timeout time.Duration) (Content, error) {
	response, err := client.Download(ctx, url, DownloadRequest{Config: config, ConnectTimeout: timeout, ReadTimeout: timeout})
	if err != nil {
		return Content{}, err
	}
	defer response.Close()
	if err := response.RaiseForStatus(); err != nil {
		return Content{}, err
	}
	data, err := response.Content()
	return Content{Data: data}, err
}
