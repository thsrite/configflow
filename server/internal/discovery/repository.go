package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func loadConfig(ctx context.Context, id string) (config, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return config{}, err
	}
	view, err := r.Config(ctx, id, nil)
	if err != nil {
		return config{}, err
	}
	return configFromModel(view)
}

func configFromModel(view *model.Config) (config, error) {
	c := config{Settings: defaultSettings()}
	if view == nil || view.Profile == nil {
		return c, fmt.Errorf("%s", msgMissingProfile)
	}
	settings, err := json.Marshal(view.Profile.DomainDiscovery)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(settings, &c.Settings); err != nil {
		return c, err
	}
	for _, record := range view.RuleLibrary {
		item, err := libraryFromModel(record)
		if err != nil {
			return c, err
		}
		c.Library = append(c.Library, item)
	}
	for _, record := range view.Profile.RuleConfigs {
		item, err := ruleFromModel(record)
		if err != nil {
			return c, err
		}
		c.Rules = append(c.Rules, item)
	}
	for _, group := range view.Profile.ProxyGroups {
		c.Groups = append(c.Groups, policyGroup{Name: group.Name})
	}
	c.Mihomo.Custom = view.Profile.Mihomo.CustomConfig
	c.Settings.normalize()
	return c, nil
}

func editProfile(ctx context.Context, id string, edit func(*config) error) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateProfile(lifecycle.Detach(ctx), id, func(tx context.Context, p *model.Profile) error {
		c, err := configFromModel(&model.Config{Profile: p})
		if err != nil {
			return err
		}
		before, err := json.Marshal(c.Rules)
		if err != nil {
			return err
		}
		if err = edit(&c); err != nil {
			return err
		}
		settings, err := json.Marshal(c.Settings)
		if err != nil {
			return err
		}
		if err = p.SetJSON("domain_discovery", settings); err != nil {
			return err
		}
		after, err := json.Marshal(c.Rules)
		if err != nil {
			return err
		}
		if string(before) != string(after) {
			return p.SetJSON("rule_configs", after)
		}
		return nil
	})
	return err
}
func editSettings(ctx context.Context, id string, edit func(*Settings) error) error {
	return editProfile(ctx, id, func(c *config) error { return edit(&c.Settings) })
}
func editLibrary(ctx context.Context, edit func(*[]libraryItem) error) error {
	return editLibraryContext(ctx, func(_ context.Context, items *[]libraryItem) error { return edit(items) })
}
func editLibraryContext(ctx context.Context, edit func(context.Context, *[]libraryItem) error) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateShared(lifecycle.Detach(ctx), func(tx context.Context, shared *model.Shared) error {
		items := make([]libraryItem, 0, len(shared.RuleLibrary))
		for _, record := range shared.RuleLibrary {
			item, err := libraryFromModel(record)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := edit(tx, &items); err != nil {
			return err
		}
		data, err := json.Marshal(items)
		if err != nil {
			return err
		}
		return shared.SetJSON("rule_library", data)
	})
}

type profile struct {
	ID   string
	Name string
}

func listProfiles(ctx context.Context) ([]profile, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	out := []profile{}
	err = r.View(ctx, func(d *model.Document) error {
		for _, p := range d.Profiles {
			out = append(out, profile{p.Key(), p.Name})
		}
		return nil
	})
	return out, err
}

func profileAgents(ctx context.Context, id string) ([]Agent, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	records, err := collaboration.registryAgents(ctx)
	if err != nil {
		return nil, err
	}
	def, err := r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	out := []Agent{}
	for _, a := range records {
		profileID := a.ProfileID
		if profileID == "" {
			profileID = def
		}
		if profileID != id || a.ServiceType != "mihomo" {
			continue
		}
		out = append(out, Agent{ID: a.ID, Name: a.Name, ProfileID: profileID, ServiceType: a.ServiceType, Enabled: a.DomainDiscoveryEnabled != nil && *a.DomainDiscoveryEnabled, Version: a.Version, Heartbeat: a.LastHeartbeat})
	}
	return out, nil
}

func libraryFromModel(record model.RuleLibraryItem) (libraryItem, error) {
	raw, err := model.Marshal(record)
	if err != nil {
		return libraryItem{}, err
	}
	item := libraryItem{ID: record.ID, Name: record.Name, SourceType: "url", Behavior: "classical", original: raw}
	if record.SourceType != nil {
		item.SourceType = *record.SourceType
	}
	if record.Behavior != nil {
		item.Behavior = *record.Behavior
	}
	if record.Content != nil {
		item.Content = *record.Content
	}
	item.originalContent = item.Content
	return item, nil
}
func ruleFromModel(record model.RuleConfig) (ruleConfig, error) {
	raw, err := model.Marshal(record)
	item := ruleConfig{ID: record.ID, ItemType: record.ItemType, RuleType: record.RuleType, Value: model.TextValue(record.Value), LibraryID: record.LibraryRuleID, Enabled: model.Flag(record.Enabled, true), original: raw}
	if record.Policy != nil {
		item.Policy = *record.Policy
	}
	return item, err
}
