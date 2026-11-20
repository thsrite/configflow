package setup

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
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
	err := store.locked(ctx, id, func(held context.Context) (err error) {
		record, err := store.read(held, id)
		if err != nil {
			return err
		}
		defer func() { err = publicError(err, record) }()
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
			result, err = publicResponse(plan, record)
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
	if err := jsondecode.Unmarshal(record.Installation, &pair); err != nil {
		return err
	}
	router.ProxyGatewayAgentID, router.DNSAgentID = pair.Mihomo.ID, pair.OxiDNS.ID
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.repository.UpdateProfile(ctx, record.ProfileID, func(commit context.Context, current *model.Profile) error {
		state, marker := routersplit.ReadMarkerProfile(current)
		if state == routersplit.StateValid && len(router.IPRuleSets) == 0 {
			router.IPRuleSets = append([]string{}, marker.RouteRuleSetIDs...)
		}
		var profile struct {
			Routers struct {
				Items []json.RawMessage `json:"items"`
			} `json:"routers"`
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if err = jsondecode.Unmarshal(raw, &profile); err != nil {
			return err
		}
		proposed, err := json.Marshal(router)
		if err != nil {
			return err
		}
		found := false
		for _, raw := range profile.Routers.Items {
			var identity struct {
				ID string `json:"id"`
			}
			if err = jsondecode.Unmarshal(raw, &identity); err != nil {
				return err
			}
			if identity.ID == router.ID {
				found = true
				if !equalJSONValue(raw, proposed) {
					return failure(msgDraftConflict, 409)
				}
			}
		}
		if !found {
			profile.Routers.Items = append(profile.Routers.Items, proposed)
		}
		fields, err := encodeFields(current)
		if err != nil {
			return err
		}
		routerFields := map[string]json.RawMessage{}
		if len(fields["routers"]) > 0 && string(fields["routers"]) != "null" {
			if err := json.Unmarshal(fields["routers"], &routerFields); err != nil {
				return err
			}
		}
		routerFields["items"], err = json.Marshal(profile.Routers.Items)
		if err != nil {
			return err
		}
		fields["routers"], err = json.Marshal(routerFields)
		if err != nil {
			return err
		}
		if err := current.SetJSON("routers", fields["routers"]); err != nil {
			return err
		}
		return commit.Err()
	})
	if err != nil {
		return err
	}
	record.RouterID, record.RouterMode = &router.ID, pointer("managed")
	return record.clear("router_draft")
}
