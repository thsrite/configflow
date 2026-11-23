package rulelibrary

// Record DTOs delegate sources and coupled transactions to their typed owners.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/convert"
	"configflow/internal/ctxx"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

type jsonValue = json.RawMessage

func readBody(c *httpx.Ctx) (record, bool, error) {
	return httpx.BindObject[record](c, false)
}
func sharedConfig(ctx context.Context) (*model.Shared, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.Shared(ctx)
}

// libraryItems is the catalog's rule library as JSON members per item: editors
// compare, validate and revise library items as sent (see record).
func libraryItems(shared *model.Shared) ([]record, error) {
	out := make([]record, len(shared.RuleLibrary))
	for i, item := range shared.RuleLibrary {
		data, err := model.Marshal(item)
		if err == nil {
			err = out[i].UnmarshalJSON(data)
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// setLibrary stores items as the catalog's rule library. The catalog decodes
// them strictly: a member of another type is refused with the repository's
// message for the list.
func setLibrary(shared *model.Shared, items []record) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return shared.SetJSON("rule_library", raw)
}
func systemConfig(ctx context.Context) (*model.SystemConfig, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.SystemConfig(ctx)
}
func editShared(ctx context.Context, fn func(context.Context, *model.Shared) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := r.UpdateShared(ctx, func(tx context.Context, shared *model.Shared) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		if err := fn(tx, shared); err != nil {
			return err
		}
		return tx.Err()
	}); err != nil {
		return err
	}
	repo.ResetConfigContext(ctx)
	return nil
}
func editSystem(ctx context.Context, fn func(*model.System) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := r.UpdateSystem(ctx, func(tx context.Context, system *model.System) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		if err := fn(system); err != nil {
			return err
		}
		return tx.Err()
	}); err != nil {
		return err
	}
	repo.ResetConfigContext(ctx)
	return nil
}

// validateContent checks the request as sent (O18 T1c: the rule record
// cannot hold members of the wrong JSON type).
func validateContent(item record, old *record) error {
	raw, err := item.MarshalJSON()
	if err != nil {
		return err
	}
	var previous json.RawMessage
	if old != nil {
		if previous, err = old.MarshalJSON(); err != nil {
			return err
		}
	}
	return rulefetch.ValidateContentJSON(raw, previous)
}
func (s *Service) readSource(ctx context.Context, item record) ([]byte, error) {
	rule, err := ruleRecord(item)
	if err != nil {
		return nil, err
	}
	content, err := rulefetch.ReadSource(ctx, rule, s.client)
	if err != nil || content == nil {
		return nil, err
	}
	return content.Data, nil
}
func create(ctx context.Context, item record, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(item)
	var value model.RuleLibraryItem
	if err == nil {
		err = json.Unmarshal(data, &value)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = r.CreateRuleLibraryRecord(ctx, value, content)

	return err
}

// hasIP reports whether a library item has (or may have) IP entries, as the
// rules page shows it for a rule-set reference.
func hasIP(item record, shared *model.Shared) (bool, error) {
	rule, err := ruleRecord(item)
	if err != nil {
		return false, err
	}
	rule.ItemType, rule.LibraryRuleID = "ruleset", text(item, "id")
	profile, err := oxidns.NewTypedProfile(&model.Config{Profile: &model.Profile{}, RuleLibrary: shared.RuleLibrary,
		Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations})
	if err != nil {
		return false, err
	}
	return oxidns.RuleHasIPPart(rule, profile), nil
}
func inspect(content string, lines []string, body record) (*rulefetch.Inspection, error) {
	if len(lines) > 0 {
		_, payload, err := rulefetch.ReadRulePayload(content)
		if err != nil {
			return nil, err
		}
		content, err = rulefetch.AppendRuleLines(content, lines, payload)
		if err != nil {
			return nil, err
		}
	}
	out, err := rulefetch.Inspect(content, text(body, "behavior"), optionalFormat(body, "format"))
	if err != nil {
		return nil, err
	}
	if text(body, "behavior") == "classical" {
		out.Content = content
		out.Changes = []rulefetch.Change{}
	}
	return out, nil
}

func optionalFormat(r record, key string) string {
	raw, ok := r.Get(key)
	if !ok {
		return ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var out string
	if json.Unmarshal(trimmed, &out) != nil {
		return "\x00"
	}
	return out
}

// reorder applies a reorder request; a malformed request is reported as
// the only missing ID, as before.
func reorder(items []model.RuleLibraryItem, body json.RawMessage) ([]model.RuleLibraryItem, []string) {
	request, err := rules.DecodeOrder(body, "rules")
	if err != nil {
		return items, []string{err.Error()}
	}
	return rules.Reorder(items, request.IDs, request.Position, func(item model.RuleLibraryItem) string { return item.ID })
}
func missingText(ids []string) string { return rules.IdentifierList(ids) }

func cachePath(ctx context.Context, item record) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	raw, err := ruleRecord(item)
	if err != nil {
		return "", err
	}
	return r.SharedPath("rules/" + rules.CacheFilename(raw))
}
func usage(ctx context.Context, id string) (int, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	refs, err := r.ResourceUsage(ctx, "rule_library", id)
	return len(refs), err
}

func readCache(ctx context.Context, item record) ([]byte, error) {
	raw, err := ruleRecord(item)
	if err != nil {
		return nil, err
	}
	return rulefetch.ReadCache(ctx, raw, nil)
}
func writeCache(ctx context.Context, item record, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := ruleRecord(item)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = rulefetch.WriteCache(ctx, raw, rulefetch.Content{Data: content}, nil)
	return err
}
func forget(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The status owner rechecks cancellation after leaf-lock admission.
	return taskstatus.New(r.DataDir()).ForgetContext(ctx, refresh.RuleTaskKey(id))
}
func removeCache(ctx context.Context, item record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := cachePath(ctx, item)
	if err != nil {
		return err
	}
	for _, name := range []string{path, path + ".invalid"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func removeLegacy(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := rulefetch.GetRulesDir(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = os.Remove(filepath.Join(root, rules.SanitizeRuleName(name)+".list"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func editWithProfiles(ctx context.Context, changes []sourceChange, edit func(context.Context, *model.Shared) error, base string) ([]profileWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warnings := []profileWarning{}
	applicable := []sourceChange{}
	selector := func(shared *model.Shared, profiles []*model.Profile) ([]string, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := libraryItems(shared)
		if err != nil {
			return nil, err
		}
		applicable = nil
		for _, change := range changes {
			i := find(items, text(change.Old, "id"))
			if i >= 0 && same(items[i], change.Old) {
				applicable = append(applicable, change)
			}
		}
		ids := []string{}
		for _, profile := range profiles {
			settings, err := profile.OxiDNS.Node()
			if err != nil {
				return nil, err
			}
			if oxidns.SettingsText(settings, "config_mode") != "native" {
				continue
			}
			routing, downloads, err := affects(profile, applicable)
			if err != nil {
				return nil, err
			}
			if routing || downloads {
				ids = append(ids, profile.Key())
			}
		}
		return ids, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := repo.UpdateSharedConfigs(ctx, func(tx context.Context, shared *model.Shared) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		if err := edit(tx, shared); err != nil {
			return err
		}
		return tx.Err()
	}, func(tx context.Context, id string, old, current *model.Config) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		routing, downloads, err := affects(old.Profile, applicable)
		if err != nil {
			return err
		}
		messages, err := rebuild(old, current, routing, downloads, base)
		if err != nil {
			return err
		}
		for _, message := range messages {
			warnings = append(warnings, profileWarning{id, message})
		}
		return tx.Err()
	}, selector)

	return warnings, err
}

// rebuild re-routes and rebases the native OxiDNS settings of a profile
// whose library sources changed; before is resolved against the old catalog.
func rebuild(before, after *model.Config, routing, downloads bool, base string) ([]string, error) {
	warnings := []string{}
	a, err := oxidns.NewTypedProfile(before)
	if err != nil {
		return nil, err
	}
	b, err := oxidns.NewTypedProfile(after)
	if err != nil {
		return nil, err
	}
	if routing {
		result, err := oxidns.ApplyRuleDNS(a, b, nil, oxidns.RuleDNSApplyOptions{BaseURL: base, Lenient: true})
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Changed {
			if err = after.Profile.OxiDNS.SetNode(result.Settings); err != nil {
				return nil, err
			}
			b, err = oxidns.NewTypedProfile(after)
			if err != nil {
				return nil, err
			}
		}
	}
	if downloads {
		settings, err := after.Profile.OxiDNS.Node()
		if err != nil {
			return nil, err
		}
		if content := oxidns.SettingsText(settings, "native_config"); content != "" {
			if _, err := yamlx.Parse([]byte(content)); err != nil && !errors.Is(err, yamlx.ErrSyntax) {
				return nil, repo.NewValidationError(err.Error())
			}
		}
		changed, err := oxidns.RebaseNativeRuleSources(a, b)
		if err != nil {
			return nil, err
		}
		if changed {
			if err = after.Profile.OxiDNS.SetNode(b.Settings()); err != nil {
				return nil, err
			}
		}
	}
	return warnings, nil
}

func rejectedBeforeCommit(err error) bool {
	var f *fault
	var locked *oxidns.RoutingLockedError
	_, settings := oxidns.IsSettingsError(err)
	return errors.As(err, &f) || errors.As(err, &locked) || settings || errors.Is(err, repo.ErrValidation) || errors.Is(err, repo.ErrInUse) || errors.Is(err, repo.ErrExists) || errors.Is(err, repo.ErrNotFound)
}

func (s *Service) requestStatus(ctx context.Context, address string, settings *model.SystemConfig, head bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var sysCfg model.SystemConfig
	if settings != nil {
		sysCfg = *settings
	}
	address, err := convert.GitHubURL(address, &model.Config{SystemConfig: sysCfg})
	if err != nil {
		return 0, err
	}
	method := "get"
	if head {
		method = "head"
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	response, err := s.client.Download(ctx, address, rulefetch.DownloadRequest{Config: &rulefetch.SourceConfig{Settings: sysCfg}, Method: method, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second})
	if err != nil {
		return 0, err
	}
	defer response.Close()
	if !head {
		if _, err := response.Content(); err != nil {
			return 0, err
		}
	}
	return response.StatusCode, nil
}
func (s *Service) cacheOne(ctx context.Context, item record) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := ruleRecord(item)
	if err != nil {
		return "", err
	}
	if sourceType(item) == "url" {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// A manual refresh resolves global fetch settings, just like the
		// scheduler. Passing a shared catalog here would erase proxy settings.
		_, _, err := refresh.RefreshRemote(ctx, libraryRecord(raw), "manual", refresh.RuleReadOptions{Fetch: func(ctx context.Context, url string) (rulefetch.Content, error) {
			if err := ctx.Err(); err != nil {
				return rulefetch.Content{}, err
			}
			response, err := s.client.Download(ctx, url, rulefetch.DownloadRequest{ConnectTimeout: 30 * time.Second, ReadTimeout: 30 * time.Second})
			if err != nil {
				return rulefetch.Content{}, err
			}
			defer response.Close()
			if err = response.RaiseForStatus(); err != nil {
				return rulefetch.Content{}, err
			}
			data, err := response.Content()
			return rulefetch.Content{Data: data}, err
		}})
		if err != nil {
			return "", err
		}
		return rules.CacheFilename(raw), nil
	}
	content, err := s.readSource(ctx, item)
	if err != nil {
		return "", err
	}
	if content == nil {
		return "", fail(400, msgSource)
	}
	stage, err := stageCache(ctx, item, content, s.replaceCache)
	if err != nil {
		return "", err
	}
	if err = stage.commit(ctx); err != nil {
		return "", err
	}
	return rules.CacheFilename(raw), nil
}
func describeFailure(err error) string { return refresh.FailureReason(err) }

func requestURL(body record) (string, error) {
	raw := field(body, "url")
	if !rules.EnabledField(raw, false) {
		return "", fail(400, "URL is required")
	}
	var address *string
	if json.Unmarshal(raw, &address) != nil || address == nil {
		return "", fail(500, httpx.DescInternal)
	}
	return *address, nil
}

// readCaughtBody preserves the error boundary of endpoints that deliberately
// catch JSON decoding failures. The diagnostic stays native and contains no body.
func readCaughtBody(c *httpx.Ctx) (record, bool, error) {
	body, ok, err := readBody(c)
	if err != nil {
		return record{}, false, fail(500, "invalid JSON request")
	}
	return body, ok, nil
}

func readReorderBody(c *httpx.Ctx) (record, error) {
	body, ok, err := readCaughtBody(c)
	if err != nil || ok {
		return body, err
	}
	// Python's `body or {}` defaults only empty values. A nonempty scalar or
	// array still fails before resolving IDs; do not report a missing resource.
	if boundNonempty(c) {
		return record{}, fail(500, "reorder request must be a JSON object")
	}
	return record{}, nil
}

func ruleRecord(item record) (model.RuleConfig, error) {
	raw, err := json.Marshal(item)
	var out model.RuleConfig
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
func libraryRecord(rule model.RuleConfig) model.RuleLibraryItem {
	data, _ := model.Marshal(rule)
	var item model.RuleLibraryItem
	_ = json.Unmarshal(data, &item)
	return item
}
func boundNonempty(c *httpx.Ctx) bool {
	var raw json.RawMessage
	return c.BindSilent(&raw) && rules.EnabledField(raw, false)
}
