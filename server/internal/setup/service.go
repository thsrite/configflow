package setup

import (
	"configflow/internal/jsondecode"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routersplit"
)

// Service owns wizard state for one repository. Construct a service per request;
// operation locks are shared across services for the same data directory.
type Service struct {
	repository  *repo.Repository
	catalog     *catalog
	owners      ExternalOwners
	spawner     lockx.Spawner
	routeBudget time.Duration
}

// New constructs the wizard using the bundled first-start template.
func New(r *repo.Repository) (*Service, error) {
	c, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	return &Service{repository: r, catalog: c, owners: externalOwners()}, nil
}

func bodyFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) > 32768 {
		return nil, failure(msgLargeBody, 413)
	}
	var fields map[string]json.RawMessage
	if !utf8.Valid(raw) || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, failure(msgBadBody, 400)
	}
	return fields, nil
}

func allowed(fields map[string]json.RawMessage, names ...string) bool {
	for name := range fields {
		if !slices.Contains(names, name) {
			return false
		}
	}
	return true
}

// Create reserves a stable profile identity and retries an interrupted creation.
// A disconnected caller does not abort the write; Runtime shutdown still does.
func (s *Service) Create(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fields, err := bodyFields(raw)
	if err != nil {
		return nil, err
	}
	var id, goal, name string
	_ = json.Unmarshal(fields["id"], &id)
	_ = json.Unmarshal(fields["goal"], &goal)
	_ = json.Unmarshal(fields["name"], &name)
	if goal != "router" && goal != "gateway" {
		return nil, failure(msgGoal, 400)
	}
	name = strings.TrimSpace(name)
	if length := utf8.RuneCountInString(name); length < 1 || length > 64 {
		return nil, failure(msgNameLength, 400)
	}
	store := storeFor(s.repository)
	var record *Record
	err = store.locked(ctx, id, func(held context.Context) error {
		var err error
		record, err = store.read(held, id)
		var missing *Error
		if errors.As(err, &missing) && missing.Status == 404 {
			profileID := "setup_" + id[:20]
			if _, err := s.repository.ProfileMetadataRecord(held, profileID); err == nil {
				return failure(msgIdentityConflict, 409)
			} else if !errors.Is(err, repo.ErrNotFound) {
				return err
			}
			record = &Record{ID: id, ProfileID: profileID, Name: name, Goal: goal, Step: 1, MaxStep: 1,
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), SubscriptionIDs: []string{}, NodeIDs: []string{},
				Prepared: pointer(false), ProfileRevision: pointer(model.Int(0)), Completed: pointer(false)}
			if err := store.write(held, record); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if record.Goal != goal || record.Name != name {
			return failure(msgSessionConflict, 409)
		}
		if err := held.Err(); err != nil {
			return err
		}
		_, err = s.repository.CreateProfileRecord(held, repo.ProfileCreation{ID: record.ProfileID, Name: record.Name}, "")
		if err != nil && !errors.Is(err, repo.ErrExists) {
			return err
		}
		systemConfig, err := s.repository.SystemConfig(held)
		if err != nil {
			return err
		}
		if systemConfig.ConfigToken == "" {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return fmt.Errorf("create configuration token: %w", err)
			}
			if err := held.Err(); err != nil {
				return err
			}
			err = s.repository.UpdateSystem(held, func(commit context.Context, current *model.System) error {
				if err := commit.Err(); err != nil {
					return err
				}
				if current.SystemConfig.ConfigToken == "" {
					current.SystemConfig.ConfigToken = base64.RawURLEncoding.EncodeToString(secret)
				}
				return commit.Err()
			})
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return publicRecord(record)
}

// Choice is a source identity displayed without its private connection data.
type Choice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Index is the read-only landing-page state.
type Index struct {
	Sessions               []json.RawMessage `json:"sessions"`
	Suggested              bool              `json:"suggested"`
	Subscriptions          []Choice          `json:"subscriptions"`
	Nodes                  []Choice          `json:"nodes"`
	DomainRules            []DomainOption    `json:"domain_rules"`
	RecommendedDomainRules Selections        `json:"recommended_domain_rules"`
}

// Index lists resumable sessions and available sources without creating files.
func (s *Service) Index(ctx context.Context) (Index, error) {
	out := Index{Sessions: []json.RawMessage{}, Subscriptions: []Choice{}, Nodes: []Choice{}}
	shared, err := sharedSnapshot(ctx, s.repository)
	if err != nil {
		return out, err
	}
	store := storeFor(s.repository)
	ids, err := store.ids(ctx)
	if err != nil {
		return out, err
	}
	records := []*Record{}
	for _, id := range ids {
		var record *Record
		err := store.locked(ctx, id, func(held context.Context) error {
			var err error
			record, err = store.read(held, id)
			return err
		})
		var missing *Error
		if errors.As(err, &missing) && missing.Status == 404 {
			continue
		}
		if err != nil {
			return out, err
		}
		if _, err := s.repository.ProfileMetadataRecord(ctx, record.ProfileID); errors.Is(err, repo.ErrNotFound) {
			continue
		} else if err != nil {
			return out, err
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].UpdatedAt > records[j].UpdatedAt })
	for _, record := range records {
		value, err := publicRecord(record)
		if err != nil {
			return out, err
		}
		out.Sessions = append(out.Sessions, value)
	}
	var noAgents, dismissed bool
	if err := s.repository.View(ctx, func(doc *model.Document) error {
		noAgents = len(doc.System.Agents) == 0
		dismissed = doc.System.SystemConfig.SetupSuggestionDismissed != nil && *doc.System.SystemConfig.SetupSuggestionDismissed
		return nil
	}); err != nil {
		return out, err
	}
	out.Suggested = len(records) == 0 && noAgents && !dismissed
	for _, item := range selectableSubscriptions(shared.Subscriptions) {
		out.Subscriptions = append(out.Subscriptions, Choice{item.ID, item.Name})
	}
	for i := range shared.Nodes {
		item := &shared.Nodes[i]
		if resourceEnabled(item.Enabled) {
			out.Nodes = append(out.Nodes, Choice{item.ID, item.Name})
		}
	}
	out.DomainRules = s.catalog.options(shared)
	out.RecommendedDomainRules = s.catalog.recommended(out.DomainRules)
	public, err := publicResponse(out, records...)
	if err != nil {
		return out, err
	}
	err = jsondecode.Unmarshal(public, &out)
	return out, err
}

// publicResponse applies the session's credential references to the complete
// response, including metadata added after the private record was projected.
// Keep presence booleans such as has_password intact: credential fields were
// already removed by publicRecord; this final pass masks string references.
func publicResponse(value any, records ...*Record) (json.RawMessage, error) {
	var secrets redact.Secrets
	for _, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(redact.SanitizeWithSecrets(json.RawMessage(raw), secrets, false))
}

// publicError keeps public status and owner causes while applying the same
// private-session string policy as successful responses. Internal errors keep
// using the server's generic error boundary.
func publicError(err error, record *Record) error {
	if err == nil {
		return nil
	}
	var wizard *Error
	status, message := 0, err.Error()
	switch {
	case errors.As(err, &wizard):
		status, message = wizard.Status, wizard.Message
		// This fixed readiness message contains no external data. Preserve its
		// exact contract even if a weak password happens to equal the text.
		if status == 503 && message == msgAwaitOwner {
			return err
		}
	case errors.Is(err, repo.ErrInUse), errors.Is(err, repo.ErrValidation):
		status = 409
	default:
		return err
	}
	var public struct {
		Message string `json:"message"`
	}
	public.Message = message
	raw, redactErr := publicResponse(public, record)
	if redactErr != nil {
		return fmt.Errorf("sanitize setup error: %w", redactErr)
	}
	if decodeErr := jsondecode.Unmarshal(raw, &public); decodeErr != nil {
		return fmt.Errorf("decode sanitized setup error: %w", decodeErr)
	}
	if public.Message == message {
		return err
	}
	return &Error{Status: status, Message: public.Message, Cause: err}
}

func publicRecord(record *Record) (json.RawMessage, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	secrets := redact.CollectCredentials(json.RawMessage(raw), nil)
	var hasPassword bool
	if len(record.RouterDraft) != 0 && string(record.RouterDraft) != "null" {
		var router routers.Router
		if err := json.Unmarshal(record.RouterDraft, &router); err != nil {
			return nil, err
		}
		hasPassword = router.Password != ""
	}
	if err := json.Unmarshal(redact.SanitizeCredentials(json.RawMessage(raw), secrets), &payload); err != nil {
		return nil, err
	}
	if len(record.RouterDraft) != 0 && string(record.RouterDraft) != "null" {
		cleaned := payload["router_draft"]
		var router routers.Router
		if err := json.Unmarshal(cleaned, &router); err != nil {
			return nil, err
		}
		viewState := router.View()
		// Presence is public metadata, not the password value. The shared
		// credential-key policy must run before generating this boolean.
		viewState.HasPassword = hasPassword
		view, err := json.Marshal(viewState)
		if err != nil {
			return nil, err
		}
		payload["router_draft"] = view
	}
	return json.Marshal(redact.SanitizeWithSecrets(payload, secrets, false))
}

// Get describes one session and the default-profile choice, without writing.
func (s *Service) Get(ctx context.Context, id string) (json.RawMessage, error) {
	store := storeFor(s.repository)
	var result json.RawMessage
	err := store.locked(ctx, id, func(held context.Context) (err error) {
		record, err := store.read(held, id)
		if err != nil {
			return err
		}
		defer func() { err = publicError(err, record) }()
		if _, err := s.repository.ProfileMetadataRecord(held, record.ProfileID); err != nil {
			return err
		}
		result, err = s.described(held, record)
		return err
	})
	return result, err
}

// Patch saves only progress fields; it cannot retarget the session's profile.
func (s *Service) Patch(ctx context.Context, id string, raw json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := storeFor(s.repository)
	var result json.RawMessage
	err := store.locked(ctx, id, func(held context.Context) (err error) {
		record, err := store.read(held, id)
		if err != nil {
			return err
		}
		defer func() { err = publicError(err, record) }()
		profile, err := s.repository.Profile(held, record.ProfileID)
		if err != nil {
			return err
		}
		fields, err := bodyFields(raw)
		if err != nil {
			return err
		}
		if !allowed(fields, "step", "router_id", "router_mode") {
			return failure(msgProgressFields, 400)
		}
		if value, ok := fields["step"]; ok {
			var step int64
			if len(value) == 0 || strings.ContainsAny(string(value), ".eE") || json.Unmarshal(value, &step) != nil || string(value) == "null" || step < 0 || step > int64(record.MaxStep) {
				return failure(msgProgressOrder, 400)
			}
			record.Step = model.Int(step)
		}
		if value, ok := fields["router_mode"]; ok {
			var mode string
			if json.Unmarshal(value, &mode) != nil || mode != "manual" && mode != "managed" || record.Goal != "router" {
				return failure(msgRouterMode, 400)
			}
			record.RouterMode = &mode
		}
		if value, ok := fields["router_id"]; ok {
			var id string
			if json.Unmarshal(value, &id) != nil {
				return failure(msgRouterSelection, 409)
			}
			found := false
			for _, router := range profile.Routers.Items {
				if router.ID == id {
					found = true
				}
			}
			if !found {
				return failure(msgRouterSelection, 409)
			}
			record.RouterID = &id
		}
		if err := record.clear("abandoned"); err != nil {
			return err
		}
		if err := store.write(held, record); err != nil {
			return err
		}
		result, err = s.described(held, record)
		return err
	})
	return result, err
}

func (s *Service) described(ctx context.Context, record *Record) (json.RawMessage, error) {
	raw, err := publicRecord(record)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	summary, err := s.profileSummary(ctx, record)
	if err != nil {
		return nil, err
	}
	fields["default_profile"], err = json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	if record.Goal == "router" && enabled(record.Prepared) && record.LegacyTakeover == nil {
		profile, err := s.repository.Profile(ctx, record.ProfileID)
		if err != nil {
			return nil, err
		}
		settings, err := gateway.SettingsOf(&model.Config{Profile: profile})
		if err == nil {
			state, _ := routersplit.ReadMarkerProfile(profile)
			fields["legacy_takeover"], _ = json.Marshal(state == routersplit.StateAbsent && settings.Enabled)
		} else {
			var malformed *gateway.Error
			if !errors.As(err, &malformed) {
				return nil, err
			}
		}
	}
	return publicResponse(fields, record)
}

type defaultSummary struct {
	IsDefault bool       `json:"is_default"`
	Current   Choice     `json:"current"`
	Suggested bool       `json:"suggested"`
	Leftovers *leftovers `json:"leftovers,omitempty"`
}

type leftovers struct {
	Profile       *Choice  `json:"profile"`
	Subscriptions []Choice `json:"subscriptions"`
}

func (s *Service) profileSummary(ctx context.Context, record *Record) (defaultSummary, error) {
	var out defaultSummary
	id, err := s.repository.DefaultProfileID(ctx)
	if err != nil {
		return out, err
	}
	profile, err := s.repository.ProfileMetadataRecord(ctx, id)
	if err != nil {
		return out, err
	}
	bound, err := s.repository.ProfileAgentBindingsRecord(ctx, id)
	if err != nil {
		return out, err
	}
	out.IsDefault, out.Current = id == record.ProfileID, Choice{id, profile.Name}
	out.Suggested = !out.IsDefault && len(bound.Agents) == 0
	previous := text(record.PreviousDefault)
	if !out.IsDefault || previous == "" {
		return out, nil
	}
	out.Leftovers = &leftovers{Subscriptions: []Choice{}}
	old, err := s.repository.ProfileMetadataRecord(ctx, previous)
	if err == nil {
		bindings, err := s.repository.ProfileAgentBindingsRecord(ctx, previous)
		if err != nil {
			return out, err
		}
		if len(bindings.Agents) == 0 {
			out.Leftovers.Profile = &Choice{previous, old.Name}
		}
	}
	shared, err := sharedSnapshot(ctx, s.repository)
	if err != nil {
		return out, err
	}
	templateIDs := []string{}
	for _, item := range s.catalog.Template.Subscriptions {
		templateIDs = append(templateIDs, item.ID)
	}
	slices.Sort(templateIDs)
	for _, id := range templateIDs {
		for _, item := range shared.Subscriptions {
			if item.ID != id {
				continue
			}
			usages, err := s.repository.ResourceUsage(ctx, "subscriptions", id)
			if err != nil {
				return out, err
			}
			unused := true
			for _, usage := range usages {
				unused = unused && usage.Kind == "profile" && usage.ID == previous
			}
			if unused {
				out.Leftovers.Subscriptions = append(out.Leftovers.Subscriptions, Choice{id, item.Name})
			}
		}
	}
	return out, nil
}
