// Package rulelibrary owns shared rule-source editing and connectivity checks.
// Parsing, fetching, native DNS reconstruction and storage stay with their owners.
package rulelibrary

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

type member[T any] = jsondecode.Member[T]

type libraryItemWire model.RuleLibraryItem

// libraryListItem is a stored library item annotated with has_ip_part for GET /api/rule-library.
type libraryListItem struct {
	libraryItemWire
	HasIPPart bool `json:"has_ip_part"`
}

// libraryRequest is a rule-library create/replace request body decoded with
// exact member matching. Derived display members (base_url, has_ip_part) and
// undeclared members are ignored.
type libraryRequest struct {
	ID             member[string]          `json:"id"`
	Name           member[json.RawMessage] `json:"name"`
	Enabled        member[json.RawMessage] `json:"enabled"`
	SourceType     member[string]          `json:"source_type"`
	Behavior       member[string]          `json:"behavior"`
	Format         member[string]          `json:"format"`
	URL            member[string]          `json:"url"`
	Content        member[string]          `json:"content"`
	Remark         member[json.RawMessage] `json:"remark"`
	UpdateInterval member[json.RawMessage] `json:"update_interval"`
	Policy         member[json.RawMessage] `json:"policy"`
	Target         member[json.RawMessage] `json:"target"`
}

func (r *libraryRequest) sourceType() string {
	if !r.SourceType.Present() {
		return "url"
	}
	return r.SourceType.Value
}

func (r *libraryRequest) declaration() rulefetch.Declaration {
	return rulefetch.Declaration{
		SourceType: r.SourceType,
		Behavior:   r.Behavior,
		Format:     r.Format,
		Content:    r.Content,
	}
}

func (r *libraryRequest) cleanForCreate() {
	switch r.sourceType() {
	case "content":
		r.URL = member[string]{}
	case "url":
		r.Content = member[string]{}
	}
}

func (r *libraryRequest) cleanForUpdate() {
	if r.sourceType() == "content" {
		r.URL = member[string]{}
	} else {
		r.Content = member[string]{}
	}
}

func (r *libraryRequest) decodeItem(overrideID string, inheritInterval *model.Int) (model.RuleLibraryItem, error) {
	var item model.RuleLibraryItem
	if overrideID != "" {
		item.ID = overrideID
	}
	fields := []struct {
		name string
		raw  json.RawMessage
	}{
		{"name", r.Name.Raw},
		{"enabled", r.Enabled.Raw},
		{"source_type", r.SourceType.Raw},
		{"behavior", r.Behavior.Raw},
		{"format", r.Format.Raw},
		{"url", r.URL.Raw},
		{"content", r.Content.Raw},
		{"remark", r.Remark.Raw},
		{"update_interval", r.UpdateInterval.Raw},
		{"policy", r.Policy.Raw},
		{"target", r.Target.Raw},
	}
	if overrideID == "" {
		fields = append([]struct {
			name string
			raw  json.RawMessage
		}{{"id", r.ID.Raw}}, fields...)
	}
	for _, field := range fields {
		if field.raw == nil {
			continue
		}
		if err := model.DecodeInputMember("rule_library_item", &item, field.name, field.raw); err != nil {
			return model.RuleLibraryItem{}, err
		}
	}
	if !r.UpdateInterval.Present() && inheritInterval != nil {
		item.UpdateInterval = inheritInterval
	}
	return item, nil
}

// previewItem returns a best-effort typed RuleLibraryItem for pre-transaction
// source reading and profile-selection checks; strict decoding runs on commit.
func (r *libraryRequest) previewItem(overrideID string) model.RuleLibraryItem {
	item := model.RuleLibraryItem{ID: overrideID}
	if overrideID == "" && r.ID.OK {
		item.ID = r.ID.Value
	}
	if r.Name.Present() {
		var name string
		if json.Unmarshal(r.Name.Raw, &name) == nil {
			item.Name = name
		}
	}
	if r.Enabled.Present() {
		var enabled bool
		if json.Unmarshal(r.Enabled.Raw, &enabled) == nil {
			item.Enabled = &enabled
		}
	}
	if r.SourceType.OK {
		item.SourceType = new(r.SourceType.Value)
	}
	if r.Behavior.OK {
		item.Behavior = new(r.Behavior.Value)
	} else if r.Behavior.Present() && r.Behavior.Null() {
		item.Behavior = new("")
	}
	if r.Format.OK {
		item.Format = new(r.Format.Value)
	}
	if r.URL.OK {
		item.URL = new(r.URL.Value)
	}
	if r.Content.OK {
		item.Content = new(r.Content.Value)
	}
	return item
}

type draftRule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"source_type"`
	Behavior   string `json:"behavior"`
	Format     string `json:"format"`
	Content    string `json:"content"`
	URL        string `json:"url"`
}

type draftRequest struct {
	Revision member[string]
	Content  member[string]
	exact    bool
}

func decodeDraftRequest(raw json.RawMessage) *draftRequest {
	if len(raw) == 0 || raw[0] != '{' {
		return &draftRequest{}
	}
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return &draftRequest{}
	}
	req := &draftRequest{}
	count := 0
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return &draftRequest{}
		}
		key := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return &draftRequest{}
		}
		count++
		switch key {
		case "revision":
			_ = req.Revision.UnmarshalJSON(val)
		case "content":
			_ = req.Content.UnmarshalJSON(val)
		}
	}
	req.exact = count == 2 && req.Revision.OK && req.Content.OK
	return req
}

type singleRequest struct {
	URL member[string] `json:"url"`
}

type cacheRequest struct {
	RuleIDs member[[]member[string]] `json:"rule_ids"`
}

type checkContentRequest struct {
	Content     member[string]           `json:"content"`
	AppendLines member[[]member[string]] `json:"append_lines"`
	Behavior    member[string]           `json:"behavior"`
	Format      member[string]           `json:"format"`
}

type proxyDomainsRequest struct {
	ProxyDomains member[string] `json:"proxy_domains"`
}

func find(items []model.RuleLibraryItem, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func sourceTypeOf(item model.RuleLibraryItem) string {
	if item.SourceType == nil {
		return "url"
	}
	return *item.SourceType
}

type fault struct {
	status  int
	message string
}

func (e *fault) Error() string              { return e.message }
func fail(status int, message string) error { return &fault{status, message} }

type itemResult struct {
	Success bool                  `json:"success"`
	Data    model.RuleLibraryItem `json:"data"`
}
