package rulelibrary

// Only this boundary deals with legacy repository and rule-engine objects.
import (
	"configflow/internal/convert"
	"configflow/internal/refresh"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
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
	v, err := c.JSON()
	if err != nil {
		return record{}, false, err
	}
	if _, ok := v.(*jsonx.Object); !ok {
		return record{}, false, nil
	}
	out, err := native(v)
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
	_, err := repo.UpdateSharedConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		r, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(tx, &r); err != nil {
			return nil, err
		}
		return legacy(r)
	})
	return err
}
func editSystem(ctx context.Context, fn func(*record) error) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		v, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(&v); err != nil {
			return nil, err
		}
		return legacy(v)
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
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	raw, err := legacy(item)
	if err != nil {
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
	raw, err := legacy(item)
	if err != nil {
		return err
	}
	_, err = rulefetch.WriteValidatedRuleCache(ctx, raw, content, nil)
	return err
}
func forget(ctx context.Context, id string) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return taskstatus.New(r.DataDir()).Forget(refresh.RuleSourceKey(id))
}
func removeCache(ctx context.Context, item record) error {
	path, err := cachePath(ctx, item)
	if err != nil {
		return err
	}
	for _, name := range []string{path, path + ".invalid"} {
		if err = os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func removeLegacy(ctx context.Context, name string) error {
	root, err := rulefetch.GetRulesDir(ctx)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(root, rules.SanitizeRuleName(name)+".list"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func editWithProfiles(ctx context.Context, changes []sourceChange, edit func(context.Context, *record) error, base string) ([]profileWarning, error) {
	warnings := []profileWarning{}
	applicable := []sourceChange{}
	selector := func(shared, profiles *jsonx.Object) ([]string, error) {
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
	_, _, err := repo.UpdateSharedAndConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		catalog, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = edit(tx, &catalog); err != nil {
			return nil, err
		}
		return legacy(catalog)
	}, func(_ context.Context, id string, old, current *jsonx.Object) (*jsonx.Object, error) {
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
		return legacy(updated)
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

func (s *Service) requestStatus(ctx context.Context, address string, system record, head bool) (int, error) {
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
	response, err := s.client.RequestRule(ctx, address, rulefetch.Request{Config: raw, Method: method, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second})
	if err != nil {
		return 0, err
	}
	defer response.Close()
	return response.StatusCode, nil
}
func (s *Service) cacheOne(ctx context.Context, item, catalog record) (string, error) {
	raw, err := legacy(item)
	if err != nil {
		return "", err
	}
	config, err := legacy(catalog)
	if err != nil {
		return "", err
	}
	if sourceType(item) == "url" {
		_, _, err := refresh.RefreshRuleSource(ctx, raw, "manual", refresh.RuleOptions{Config: config, Fetch: func(url string) (any, error) {
			response, err := s.client.RequestRule(ctx, url, rulefetch.Request{Config: config, ConnectTimeout: 30 * time.Second, ReadTimeout: 30 * time.Second})
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
	return rulefetch.SaveRuleToLocal(ctx, raw, s.client)
}
func describeFailure(err error) string { return refresh.DescribeFailure(err) }
