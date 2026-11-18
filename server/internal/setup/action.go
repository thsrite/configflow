package setup

import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/routers"
	"configflow/internal/routersplit"
)

// Action dispatches the wizard's explicit POST operations. HTTP HEAD never
// reaches this owner; unknown operations do not create checkpoints.
func (s *Service) Action(ctx context.Context, id, operation string, raw json.RawMessage, baseURL string) (json.RawMessage, error) {
	switch operation {
	case "source":
		return s.UpdateSources(ctx, id, raw)
	case "prepare":
		return s.Prepare(ctx, id, raw, baseURL)
	case "router-draft":
		return s.SaveRouterDraft(ctx, id, raw)
	}
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := storeFor(s.repository)
	var result json.RawMessage
	err := store.locked(ctx, id, func(held context.Context) error {
		record, err := store.read(held, id)
		if err != nil {
			return err
		}
		if _, err := s.repository.ProfileMetadata(held, record.ProfileID); err != nil {
			return err
		}
		body, err := bodyFields(raw)
		if err != nil {
			return err
		}
		if operation == "abandon" {
			dryRun, valid := readBool(body, "dry_run", false)
			if !valid || !allowed(body, "dry_run") {
				return failure(msgAbandonBody, 400)
			}
			plan, err := s.abandonPlan(held, record)
			if err != nil {
				return err
			}
			if !dryRun {
				if err := s.abandon(held, record, &plan); err != nil {
					return err
				}
			}
			result, err = json.Marshal(plan)
			return err
		}
		if operation != "check" {
			if err := record.clear("abandoned"); err != nil {
				return err
			}
		}
		switch operation {
		case "check", "finish":
			if !enabled(record.Prepared) {
				return failure(msgNeedPrepare, 409)
			}
			if operation == "finish" {
				if err := s.finish(held, record, body); err != nil {
					return err
				}
			} else {
				if _, err := s.checkInstallation(held, record); err != nil {
					return err
				}
				if len(record.RouterDraft) > 0 && string(record.RouterDraft) != "null" {
					if err := store.write(held, record); err != nil {
						return err
					}
				}
				if err := s.connectRouterDraft(held, record); err != nil {
					return err
				}
				record.Step = 4
				if record.MaxStep < 4 {
					record.MaxStep = 4
				}
			}
		default:
			return failure(msgUnknownOperation, 404)
		}
		if err := store.write(held, record); err != nil {
			return err
		}
		result, err = s.described(held, record)
		return err
	})
	return result, err
}
func (s *Service) connectRouterDraft(ctx context.Context, record *Record) error {
	if len(record.RouterDraft) == 0 || string(record.RouterDraft) == "null" {
		return nil
	}
	var router routers.Router
	if err := json.Unmarshal(record.RouterDraft, &router); err != nil {
		return err
	}
	var pair checkedInstallation
	if err := json.Unmarshal(record.Installation, &pair); err != nil {
		return err
	}
	router.ProxyGatewayAgentID, router.DNSAgentID = pair.Mihomo.ID, pair.OxiDNS.ID
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _, err := s.repository.UpdateProfileTransaction(ctx, record.ProfileID, func(commit context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if routersplit.IsFullTun(current) && len(router.IPRuleSets) == 0 {
			router.IPRuleSets = []string{}
			for _, id := range routersplit.RouteRuleSetIDs(current) {
				if value, ok := id.(string); ok {
					router.IPRuleSets = append(router.IPRuleSets, value)
				}
			}
		}
		var profile struct {
			Routers struct {
				Items []json.RawMessage `json:"items"`
			} `json:"routers"`
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &profile); err != nil {
			return nil, err
		}
		proposed, err := json.Marshal(router)
		if err != nil {
			return nil, err
		}
		found := false
		for _, raw := range profile.Routers.Items {
			var identity struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(raw, &identity); err != nil {
				return nil, err
			}
			if identity.ID == router.ID {
				found = true
				if !equalJSONValue(raw, proposed) {
					return nil, failure(msgDraftConflict, 409)
				}
			}
		}
		if !found {
			profile.Routers.Items = append(profile.Routers.Items, proposed)
		}
		fields, err := encodeFields(current)
		if err != nil {
			return nil, err
		}
		routerFields := map[string]json.RawMessage{}
		if len(fields["routers"]) > 0 && string(fields["routers"]) != "null" {
			if err := json.Unmarshal(fields["routers"], &routerFields); err != nil {
				return nil, err
			}
		}
		routerFields["items"], err = json.Marshal(profile.Routers.Items)
		if err != nil {
			return nil, err
		}
		fields["routers"], err = json.Marshal(routerFields)
		if err != nil {
			return nil, err
		}
		next, err := boundary(fields)
		if err != nil {
			return nil, err
		}
		return next, commit.Err()
	})
	if err != nil {
		return err
	}
	record.RouterID, record.RouterMode = &router.ID, pointer("managed")
	return record.clear("router_draft")
}
