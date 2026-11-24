package rulelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// Service owns shared-library transactions and injectable external transport.
type Service struct {
	repo         *repo.Repository
	client       *rulefetch.Client
	refresh      *refresh.Service
	replaceCache func(context.Context, string, string) error
}

func (s *Service) refreshSvc(ctx context.Context) (*refresh.Service, error) {
	if s != nil && s.refresh != nil {
		return s.refresh, nil
	}
	r, err := s.repository(ctx)
	if err != nil {
		return nil, err
	}
	return refresh.New(refresh.Options{Repo: r}), nil
}

func (s *Service) repository(ctx context.Context) (*repo.Repository, error) {
	if s != nil && s.repo != nil {
		return s.repo, nil
	}
	return repo.GetRepository(ctx)
}

// List annotates shared entries with the shared DNS engine's IP projection.
func (s *Service) List(ctx context.Context) ([]libraryListItem, error) {
	catalog, err := s.sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]libraryListItem, len(catalog.RuleLibrary))
	for i, item := range catalog.RuleLibrary {
		has, err := hasIP(item, catalog)
		if err != nil {
			return nil, err
		}
		items[i] = libraryListItem{libraryItemWire: libraryItemWire(item), HasIPPart: has}
	}
	return items, nil
}

// Create downloads and validates before publishing a source and its cache.
func (s *Service) Create(ctx context.Context, req *libraryRequest) (itemResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := itemResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	id := req.ID.Value
	if id == "" {
		return out, fail(400, msgID)
	}
	catalog, err := s.sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	if find(catalog.RuleLibrary, id) >= 0 {
		return out, fail(409, msgDuplicate)
	}
	if err = validateDeclaration(req.declaration(), nil); err != nil {
		return out, err
	}
	cleaned := *req
	cleaned.cleanForCreate()
	item, err := cleaned.decodeItem("", nil, false)
	if err != nil {
		return out, err
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
	held, release, err := editLease(ctx, item.ID)
	if err != nil {
		return out, err
	}
	defer release()
	ctx = held
	err = s.create(ctx, item, content)
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
func (s *Service) Check(body *checkContentRequest) (*rulefetch.Inspection, error) {
	if !body.Content.OK {
		return nil, fail(400, msgCheckBody)
	}
	var lines []string
	if body.AppendLines.Present() {
		if !body.AppendLines.OK {
			return nil, fail(400, msgAppend)
		}
		lines = make([]string, len(body.AppendLines.Value))
		for i, entry := range body.AppendLines.Value {
			if !entry.OK || strings.ContainsAny(entry.Value, "\r\n") {
				return nil, fail(400, msgAppend)
			}
			lines[i] = entry.Value
		}
	}
	return inspect(body.Content.Value, lines, body)
}

// Content returns handwritten bytes directly from the shared declaration.
func (s *Service) Content(ctx context.Context, id string) ([]byte, error) {
	catalog, err := s.sharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	i := find(catalog.RuleLibrary, id)
	if i < 0 {
		return nil, fail(404, "Rule not found")
	}
	item := catalog.RuleLibrary[i]
	if sourceTypeOf(item) != "content" {
		return nil, fail(400, "This rule does not have content")
	}
	if item.Content == nil {
		return []byte{}, nil
	}
	return []byte(*item.Content), nil
}

// Reorder resolves identifiers against the latest shared transaction snapshot.
func (s *Service) Reorder(ctx context.Context, body json.RawMessage) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	order := []string{}
	err := s.editShared(ctx, func(_ context.Context, catalog *model.Shared) error {
		ordered, missing := reorder(catalog.RuleLibrary, body)
		if len(missing) > 0 {
			return fail(404, fmt.Sprintf(msgOrderMissing, missingText(missing)))
		}
		order = order[:0]
		for _, item := range ordered {
			order = append(order, item.ID)
		}
		catalog.RuleLibrary = ordered
		return nil
	})
	return order, err
}

// ProxyDomains reads the configured GitHub forwarding domain.
func (s *Service) ProxyDomains(ctx context.Context) (jsonValue, error) {
	settings, err := s.systemConfig(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(settings.GitHubProxyDomain)
	return jsonValue(raw), err
}

// SetProxyDomains preserves other system settings in the same transaction.
func (s *Service) SetProxyDomains(ctx context.Context, body *proxyDomainsRequest) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := ""
	if body.ProxyDomains.Present() {
		if !body.ProxyDomains.OK {
			return "", fail(500, "proxy_domains must be a string")
		}
		value = body.ProxyDomains.Value
	}
	value = strings.TrimSpace(value)
	err := s.editSystem(ctx, func(system *model.System) error {
		system.SystemConfig.GitHubProxyDomain = value
		return nil
	})
	return value, err
}
