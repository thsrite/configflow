package rulelibrary

// Only this boundary deals with legacy repository and rule-engine objects.
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
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

type jsonValue = json.RawMessage

func native(v any) (record, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return record{}, err
	}
	var out record
	err = json.Unmarshal(raw, &out)
	return out, err
}
func legacy(r record) (*jsonx.Object, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	out, ok := value.(*jsonx.Object)
	if !ok {
		return nil, errors.New("expected configuration object")
	}
	return out, nil
}
func readBody(c *httpx.Ctx) (record, bool, error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return record{}, false, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return record{}, false, nil
	}
	var out record
	err := json.Unmarshal(raw, &out)
	return out, true, err
}
func sharedConfig(ctx context.Context) (record, error) {
	r, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return record{}, err
	}
	return native(r)
}
func systemConfig(ctx context.Context) (record, error) {
	r, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return record{}, err
	}
	return native(r)
}
func editShared(ctx context.Context, fn func(context.Context, *record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := repo.UpdateSharedConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		// The repository's SQL batch is atomic; this owner's callback and
		// file staging still stop with its original work lifetime.
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		r, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(tx, &r); err != nil {
			return nil, err
		}
		next, err := legacy(r)
		if err != nil {
			return nil, err
		}
		if err = tx.Err(); err != nil {
			return nil, err
		}
		return next, nil
	})
	return err
}
func editSystem(ctx context.Context, fn func(*record) error) error {
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
	_, err = r.UpdateSystemTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		v, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(&v); err != nil {
			return nil, err
		}
		next, err := legacy(v)
		if err != nil {
			return nil, err
		}
		if err = tx.Err(); err != nil {
			return nil, err
		}
		return next, nil
	})
	return err
}
func validateContent(item record, old *record) error {
	r, err := legacy(item)
	if err != nil {
		return err
	}
	var previous *jsonx.Object
	if old != nil {
		previous, err = legacy(*old)
		if err != nil {
			return err
		}
	}
	return rulefetch.ValidateDomainContent(r, previous)
}
func (s *Service) readSource(ctx context.Context, item record) ([]byte, error) {
	raw, err := legacy(item)
	if err != nil {
		return nil, err
	}
	v, err := rulefetch.ReadRuleSourceContent(ctx, raw, s.client)
	if err != nil {
		return nil, err
	}
	switch content := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(content), nil
	case []byte:
		return content, nil
	default:
		return nil, errors.New("invalid source content")
	}
}
func create(ctx context.Context, item record, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	raw, err := legacy(item)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = r.CreateRuleLibraryItem(ctx, raw, content)
	return err
}
func hasIP(item, catalog record) (bool, error) {
	if err := put(&item, "itemType", "ruleset"); err != nil {
		return false, err
	}
	if err := put(&item, "library_rule_id", text(item, "id")); err != nil {
		return false, err
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return false, err
	}
	node, err := oxidns.FromJSON(raw)
	if err != nil {
		return false, err
	}
	raw, err = json.Marshal(catalog)
	if err != nil {
		return false, err
	}
	profile, err := oxidns.ProfileFromJSON(raw)
	if err != nil {
		return false, err
	}
	return oxidns.HasIPPart(node, profile), nil
}
func inspect(content string, lines []string, body record) (record, error) {
	if len(lines) > 0 {
		_, payload, err := rulefetch.ReadRulePayload(content)
		if err != nil {
			return record{}, err
		}
		content, err = rulefetch.AppendRuleLines(content, lines, payload)
		if err != nil {
			return record{}, err
		}
	}
	r, err := legacy(body)
	if err != nil {
		return record{}, err
	}
	out, err := rulefetch.InspectDomainContent(content, r.Value("behavior"), r.Value("format"))
	if err != nil {
		return record{}, err
	}
	if text(body, "behavior") == "classical" {
		out.Set("content", content)
		out.Set("changes", []any{})
	}
	return native(out)
}
func reorder(items []record, body record) ([]record, []string, error) {
	current := make([]any, len(items))
	for i, item := range items {
		r, err := legacy(item)
		if err != nil {
			return nil, nil, err
		}
		current[i] = r
	}
	r, err := legacy(body)
	if err != nil {
		return nil, nil, err
	}
	ordered, missing := rules.ResolveNewOrder(current, r, "rules")
	out := make([]record, len(ordered))
	for i, item := range ordered {
		out[i], err = native(item)
		if err != nil {
			return nil, nil, err
		}
	}
	return out, missing, nil
}
func missingText(ids []string) string {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return jsonx.Repr(out)
}

func cachePath(ctx context.Context, item record) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	raw, err := legacy(item)
	if err != nil {
		return "", err
	}
	return r.SharedPath("rules/" + rules.RuleCacheFilename(raw))
}
func usage(ctx context.Context, id string) (int, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	refs, err := r.GetResourceUsage(ctx, "rule_library", id)
	return len(refs), err
}

func readCache(ctx context.Context, item record) ([]byte, error) {
	raw, err := legacy(item)
	if err != nil {
		return nil, err
	}
	return rulefetch.ReadValidatedRuleCache(ctx, raw, nil)
}
func writeCache(ctx context.Context, item record, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := legacy(item)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = rulefetch.WriteValidatedRuleCache(ctx, raw, content, nil)
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
	// O3-6 owns the future ForgetContext cutover and the guard inside its lock.
	return taskstatus.New(r.DataDir()).Forget(refresh.RuleSourceKey(id))
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

func editWithProfiles(ctx context.Context, changes []sourceChange, edit func(context.Context, *record) error, base string) ([]profileWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warnings := []profileWarning{}
	applicable := []sourceChange{}
	selector := func(shared, profiles *jsonx.Object) ([]string, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		catalog, err := native(shared)
		if err != nil {
			return nil, err
		}
		library, err := rows(catalog, "rule_library")
		if err != nil {
			return nil, err
		}
		applicable = nil
		for _, change := range changes {
			i := find(library, text(change.Old, "id"))
			if i >= 0 && same(library[i], change.Old) {
				applicable = append(applicable, change)
			}
		}
		ids := []string{}
		for id, raw := range profiles.All() {
			profile, err := native(raw)
			if err != nil {
				return nil, err
			}
			if text(child(profile, "oxidns"), "config_mode") != "native" {
				continue
			}
			routing, downloads, err := affects(profile, applicable)
			if err != nil {
				return nil, err
			}
			if routing || downloads {
				ids = append(ids, id)
			}
		}
		return ids, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _, err := repo.UpdateSharedAndConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		catalog, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = edit(tx, &catalog); err != nil {
			return nil, err
		}
		next, err := legacy(catalog)
		if err != nil {
			return nil, err
		}
		if err = tx.Err(); err != nil {
			return nil, err
		}
		return next, nil
	}, func(tx context.Context, id string, old, current *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		before, err := native(old)
		if err != nil {
			return nil, err
		}
		after, err := native(current)
		if err != nil {
			return nil, err
		}
		routing, downloads, err := affects(before, applicable)
		if err != nil {
			return nil, err
		}
		updated, messages, err := rebuild(before, after, routing, downloads, base)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			warnings = append(warnings, profileWarning{id, message})
		}
		next, err := legacy(updated)
		if err != nil {
			return nil, err
		}
		if err = tx.Err(); err != nil {
			return nil, err
		}
		return next, nil
	}, selector)
	return warnings, err
}
func dnsProfile(p record) (*oxidns.Profile, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(raw)
}
func rebuild(before, after record, routing, downloads bool, base string) (record, []string, error) {
	warnings := []string{}
	a, err := dnsProfile(before)
	if err != nil {
		return after, nil, err
	}
	b, err := dnsProfile(after)
	if err != nil {
		return after, nil, err
	}
	if routing {
		result, err := oxidns.ApplyRuleDNS(a, b, nil, oxidns.RuleDNSApplyOptions{BaseURL: base, Lenient: true})
		if err != nil {
			return after, nil, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Changed {
			if err = after.Set("oxidns", oxidns.ToJSON(result.Settings)); err != nil {
				return after, nil, err
			}
			b, err = dnsProfile(after)
			if err != nil {
				return after, nil, err
			}
		}
	}
	if downloads {
		if content := text(child(after, "oxidns"), "native_config"); content != "" {
			if _, err := yamlx.Parse([]byte(content)); err != nil && !errors.Is(err, yamlx.ErrSyntax) {
				return after, nil, repo.NewValidationError(err.Error())
			}
		}
		changed, err := oxidns.RebaseNativeRuleSources(a, b)
		if err != nil {
			return after, nil, err
		}
		if changed {
			if err = after.Set("oxidns", oxidns.ToJSON(b.Settings())); err != nil {
				return after, nil, err
			}
		}
	}
	return after, warnings, nil
}

func rejectedBeforeCommit(err error) bool {
	var f *fault
	var locked *oxidns.RoutingLockedError
	_, settings := oxidns.IsSettingsError(err)
	return errors.As(err, &f) || errors.As(err, &locked) || settings || errors.Is(err, repo.ErrValidation) || errors.Is(err, repo.ErrInUse) || errors.Is(err, repo.ErrExists) || errors.Is(err, repo.ErrNotFound)
}

func (s *Service) requestStatus(ctx context.Context, address string, system record, head bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	raw, err := legacy(system)
	if err != nil {
		return 0, err
	}
	address, err = convert.ApplyGithubProxyDomain(address, raw)
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
	response, err := s.client.RequestRule(ctx, address, rulefetch.Request{Config: raw, Method: method, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second})
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
	raw, err := legacy(item)
	if err != nil {
		return "", err
	}
	if sourceType(item) == "url" {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// A manual refresh resolves global fetch settings, just like the
		// scheduler. Passing a shared catalog here would erase proxy settings.
		_, _, err := refresh.RefreshRuleSource(ctx, raw, "manual", refresh.RuleOptions{Fetch: func(url string) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			response, err := s.client.RequestRule(ctx, url, rulefetch.Request{ConnectTimeout: 30 * time.Second, ReadTimeout: 30 * time.Second})
			if err != nil {
				return nil, err
			}
			defer response.Close()
			if err = response.RaiseForStatus(); err != nil {
				return nil, err
			}
			return response.Content()
		}})
		if err != nil {
			return "", err
		}
		return rules.RuleCacheFilename(raw), nil
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
	return rules.RuleCacheFilename(raw), nil
}
func describeFailure(err error) string { return refresh.DescribeFailure(err) }

func requestURL(body record) (string, error) {
	value, err := jsonx.Parse(field(body, "url"))
	if err != nil {
		return "", err
	}
	if !jsonx.Truthy(value) {
		return "", fail(400, "URL is required")
	}
	address, ok := value.(string)
	if !ok {
		return "", fail(500, httpx.DescInternal)
	}
	return address, nil
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
	if jsonx.Truthy(c.JSONSilent()) {
		return record{}, fail(500, "reorder request must be a JSON object")
	}
	return record{}, nil
}
