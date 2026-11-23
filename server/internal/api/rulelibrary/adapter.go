package rulelibrary

// Typed DTOs delegate sources and coupled transactions to their typed owners.
import (
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

func decodeRequestObject[T any](c *httpx.Ctx) (*T, bool, error) {
	out, ok, err := httpx.BindObject[T](c, false)
	if err != nil || !ok {
		return nil, ok, err
	}
	return &out, true, nil
}
func sharedConfig(ctx context.Context) (*model.Shared, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.Shared(ctx)
}

type libraryErrorItem struct {
	ID     string          `json:"id"`
	Name   json.RawMessage `json:"name,omitempty"`
	Policy json.RawMessage `json:"policy,omitempty"`
	Target json.RawMessage `json:"target,omitempty"`
}

// libraryTypeError reports a member of a replaced item (existing[index]) the
// item record cannot hold, with the repository's message for the resulting list.
func libraryTypeError(existing []model.RuleLibraryItem, index int, req *libraryRequest, id string, cause error) error {
	failed, err := json.Marshal(libraryErrorItem{
		ID:     id,
		Name:   req.Name.Raw,
		Policy: req.Policy.Raw,
		Target: req.Target.Raw,
	})
	if err != nil {
		return err
	}
	list := make([]json.RawMessage, 0, len(existing))
	for i, item := range existing {
		if i == index {
			list = append(list, failed)
			continue
		}
		data, err := model.Marshal(item)
		if err != nil {
			return err
		}
		list = append(list, data)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return repo.InputError(&model.MemberTypeError{Record: "shared", Member: "rule_library", Value: raw, Err: cause})
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

// validateDeclaration checks the request as sent (O18 T1c: the rule record
// cannot hold members of the wrong JSON type).
func validateDeclaration(sent rulefetch.Declaration, old *model.RuleLibraryItem) error {
	var before *rulefetch.Declaration
	if old != nil {
		before = new(rulefetch.DeclarationOf(ruleRecord(*old)))
	}
	return rulefetch.ValidateDeclaration(sent, before)
}

func validateItemContent(item model.RuleLibraryItem, old *model.RuleLibraryItem) error {
	var before *model.RuleConfig
	if old != nil {
		before = new(ruleRecord(*old))
	}
	return rulefetch.ValidateContent(ruleRecord(item), before)
}

func (s *Service) readSource(ctx context.Context, item model.RuleLibraryItem) ([]byte, error) {
	content, err := rulefetch.ReadSource(ctx, ruleRecord(item), s.client)
	if err != nil || content == nil {
		return nil, err
	}
	return content.Data, nil
}
func create(ctx context.Context, item model.RuleLibraryItem, content []byte) error {
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
	_, err = r.CreateRuleLibraryRecord(ctx, item, content)
	return err
}

// hasIP reports whether a library item has (or may have) IP entries, as the
// rules page shows it for a rule-set reference.
func hasIP(item model.RuleLibraryItem, shared *model.Shared) (bool, error) {
	rule := ruleRecord(item)
	rule.ItemType, rule.LibraryRuleID = "ruleset", item.ID
	profile, err := oxidns.NewTypedProfile(&model.Config{Profile: &model.Profile{}, RuleLibrary: shared.RuleLibrary,
		Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations})
	if err != nil {
		return false, err
	}
	return oxidns.HasIPPart(rule, profile), nil
}
func inspect(content string, lines []string, body *checkContentRequest) (*rulefetch.Inspection, error) {
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
	format := ""
	if !body.Format.Null() {
		if body.Format.OK {
			format = body.Format.Value
		} else {
			format = "\x00"
		}
	}
	behavior := body.Behavior.Value
	out, err := rulefetch.Inspect(content, behavior, format)
	if err != nil {
		return nil, err
	}
	if behavior == "classical" {
		out.Content = content
		out.Changes = []rulefetch.Change{}
	}
	return out, nil
}

// reorder applies a reorder request; a malformed request is reported as
// the only missing ID, as before.
func reorder(items []model.RuleLibraryItem, body json.RawMessage) ([]model.RuleLibraryItem, []string) {
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	request, err := rules.DecodeOrder(body, "rules")
	if err != nil {
		return items, []string{err.Error()}
	}
	return rules.Reorder(items, request.IDs, request.Position, func(item model.RuleLibraryItem) string { return item.ID })
}
func missingText(ids []string) string { return rules.IdentifierList(ids) }

func cachePath(ctx context.Context, item model.RuleLibraryItem) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	return r.SharedPath("rules/" + rules.CacheFilename(ruleRecord(item)))
}
func usage(ctx context.Context, id string) (int, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	refs, err := r.ResourceUsage(ctx, "rule_library", id)
	return len(refs), err
}

func readCache(ctx context.Context, item model.RuleLibraryItem) ([]byte, error) {
	return rulefetch.ReadCache(ctx, ruleRecord(item), nil)
}
func writeCache(ctx context.Context, item model.RuleLibraryItem, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := rulefetch.WriteCache(ctx, ruleRecord(item), rulefetch.Content{Data: content}, nil)
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
func removeCache(ctx context.Context, item model.RuleLibraryItem) error {
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
		applicable = nil
		for _, change := range changes {
			i := find(shared.RuleLibrary, change.Old.ID)
			if i >= 0 && same(shared.RuleLibrary[i], change.Old) {
				applicable = append(applicable, change)
			}
		}
		ids := []string{}
		for _, profile := range profiles {
			head, err := oxidns.ReadSettingsHead(profile.OxiDNS)
			if err != nil {
				return nil, err
			}
			if head.Mode != "native" {
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
		head, err := oxidns.ReadSettingsHead(after.Profile.OxiDNS)
		if err != nil {
			return nil, err
		}
		if content := head.NativeConfig; content != "" {
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
func (s *Service) cacheOne(ctx context.Context, item model.RuleLibraryItem) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw := ruleRecord(item)
	if sourceTypeOf(item) == "url" {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// A manual refresh resolves global fetch settings, just like the
		// scheduler. Passing a shared catalog here would erase proxy settings.
		_, _, err := refresh.RefreshRemote(ctx, item, "manual", refresh.RuleReadOptions{Fetch: func(ctx context.Context, url string) (rulefetch.Content, error) {
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

func requestURL(body *singleRequest) (string, error) {
	raw := body.URL.Raw
	if raw == nil {
		raw = json.RawMessage("null")
	}
	if !rules.EnabledField(raw, false) {
		return "", fail(400, "URL is required")
	}
	if !body.URL.OK {
		return "", fail(500, httpx.DescInternal)
	}
	return body.URL.Value, nil
}

// decodeCaughtObject preserves the error boundary of endpoints that deliberately
// catch JSON decoding failures. The diagnostic stays native and contains no body.
func decodeCaughtObject[T any](c *httpx.Ctx) (*T, bool, error) {
	body, ok, err := decodeRequestObject[T](c)
	if err != nil {
		return nil, false, fail(500, "invalid JSON request")
	}
	return body, ok, nil
}

func readReorderBody(c *httpx.Ctx) (json.RawMessage, error) {
	raw, ok, err := httpx.BindObject[json.RawMessage](c, false)
	if err != nil {
		return nil, fail(500, "invalid JSON request")
	}
	if ok {
		return raw, nil
	}
	// Python's `body or {}` defaults only empty values. A nonempty scalar or
	// array still fails before resolving IDs; do not report a missing resource.
	if boundNonempty(c) {
		return nil, fail(500, "reorder request must be a JSON object")
	}
	return json.RawMessage("{}"), nil
}

func ruleRecord(item model.RuleLibraryItem) model.RuleConfig {
	return model.RuleConfig{
		ID:         item.ID,
		Name:       item.Name,
		Enabled:    item.Enabled,
		SourceType: item.SourceType,
		Behavior:   item.Behavior,
		Format:     item.Format,
		URL:        item.URL,
		Content:    item.Content,
		Remark:     item.Remark,
	}
}
func boundNonempty(c *httpx.Ctx) bool {
	var raw json.RawMessage
	return c.BindSilent(&raw) && rules.EnabledField(raw, false)
}
