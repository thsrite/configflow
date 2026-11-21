package rulelibrary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// Service owns shared-library transactions and injectable external transport.
type Service struct {
	client       *rulefetch.Client
	replaceCache func(context.Context, string, string) error
}

// List annotates shared entries with the shared DNS engine's IP projection.
func (s *Service) List(ctx context.Context) ([]record, error) {
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	items, err := rows(catalog, "rule_library")
	if err != nil {
		return nil, err
	}
	for i := range items {
		has, err := hasIP(items[i], catalog)
		if err != nil {
			return nil, err
		}
		if err = put(&items[i], "has_ip_part", has); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// Create downloads and validates before publishing a source and its cache.
func (s *Service) Create(ctx context.Context, item record) (itemResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := itemResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	id := text(item, "id")
	if id == "" {
		return out, fail(400, msgID)
	}
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	library, err := rows(catalog, "rule_library")
	if err != nil {
		return out, err
	}
	if find(library, id) >= 0 {
		return out, fail(409, msgDuplicate)
	}
	item.Delete("has_ip_part")
	if err = validateContent(item, nil); err != nil {
		return out, err
	}
	if sourceType(item) == "content" {
		item.Delete("url")
	} else if sourceType(item) == "url" {
		item.Delete("content")
	}
	content, err := s.readSource(ctx, item)
	if err != nil {
		if rulefetch.IsRequestException(err) {
			return out, fail(502, msgDownloadCreate)
		}
		return out, err
	}
	if content == nil {
		return out, fail(400, msgSource)
	}
	// Download before admission so a slow URL creation cannot block an inline
	// creation of the same ID. Serialize the actual creation with edit/delete
	// cleanup so a new generation never lands under a previous owner's lease.
	held, release, err := editLease(ctx, item)
	if err != nil {
		return out, err
	}
	defer release()
	ctx = held
	err = create(ctx, item, content)
	if err != nil {
		if errors.Is(err, repo.ErrRuleLibraryCreationUncertain) {
			return out, err
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return out, fail(500, msgSaveCreate)
		}
		return out, err
	}
	out.Data = item
	return out, nil
}

// Check validates editable content and appends complete lines via yamlx.
func (s *Service) Check(body record) (record, error) {
	content, ok := stringField(body, "content")
	if !ok {
		return record{}, fail(400, msgCheckBody)
	}
	lines, err := stringsField(body, "append_lines")
	if err != nil {
		return record{}, fail(400, msgAppend)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return record{}, fail(400, msgAppend)
		}
	}
	return inspect(content, lines, body)
}

// Content returns handwritten bytes directly from the shared declaration.
func (s *Service) Content(ctx context.Context, id string) ([]byte, error) {
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	library, err := rows(catalog, "rule_library")
	if err != nil {
		return nil, err
	}
	i := find(library, id)
	if i < 0 {
		return nil, fail(404, "Rule not found")
	}
	if sourceType(library[i]) != "content" {
		return nil, fail(400, "This rule does not have content")
	}
	if !library[i].Has("content") {
		return []byte{}, nil
	}
	content, ok := stringField(library[i], "content")
	if !ok {
		return nil, fail(500, "invalid stored rule content")
	}
	return []byte(content), nil
}

// Reorder resolves identifiers against the latest shared transaction snapshot.
func (s *Service) Reorder(ctx context.Context, body record) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	order := []string{}
	err := editShared(ctx, func(_ context.Context, catalog *record) error {
		library, err := rows(*catalog, "rule_library")
		if err != nil {
			return err
		}
		ordered, missing, err := reorder(library, body)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return fail(404, fmt.Sprintf(msgOrderMissing, missingText(missing)))
		}
		for _, row := range ordered {
			order = append(order, text(row, "id"))
		}
		return put(catalog, "rule_library", ordered)
	})
	return order, err
}

// ProxyDomains reads the configured GitHub forwarding domain.
func (s *Service) ProxyDomains(ctx context.Context) (jsonValue, error) {
	settings, err := systemConfig(ctx)
	if err != nil {
		return nil, err
	}
	raw, ok, err := settings.MemberJSON("github_proxy_domain")
	if err != nil {
		return nil, err
	}
	if !ok {
		return jsonValue(`""`), nil
	}
	return raw, nil
}

// SetProxyDomains preserves other system settings in the same transaction.
func (s *Service) SetProxyDomains(ctx context.Context, body record) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := ""
	if body.Has("proxy_domains") {
		var ok bool
		value, ok = stringField(body, "proxy_domains")
		if !ok {
			return "", fail(500, "proxy_domains must be a string")
		}
	}
	value = strings.TrimSpace(value)
	err := editSystem(ctx, func(system *record) error {
		config := child(*system, "system_config")
		if err := put(&config, "github_proxy_domain", value); err != nil {
			return err
		}
		return put(system, "system_config", config)
	})
	return value, err
}
