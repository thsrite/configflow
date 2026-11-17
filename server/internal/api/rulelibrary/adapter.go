package rulelibrary

// Only this boundary deals with legacy repository and rule-engine objects.
import (
	"context"
	"encoding/json"
	"errors"

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
