package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

type abandonPlan struct {
	SessionID        string  `json:"session_id"`
	Name             string  `json:"name"`
	Profile          Choice  `json:"profile"`
	DeleteProfile    bool    `json:"delete_profile"`
	KeptReason       *string `json:"kept_reason"`
	Guidance         string  `json:"guidance"`
	Subscription     *Choice `json:"subscription"`
	SubscriptionKept bool    `json:"subscription_kept,omitempty"`
}

func (s *Service) abandonPlan(ctx context.Context, record *Record) (abandonPlan, error) {
	out := abandonPlan{SessionID: record.ID, Name: record.Name, Profile: Choice{record.ProfileID, record.Name}, DeleteProfile: true}
	if enabled(record.Completed) {
		return out, failure(msgAlreadyCompleted, 409)
	}
	profile, err := s.repository.Profile(ctx, record.ProfileID)
	if err != nil {
		return out, err
	}
	if name := profile.Name; name != "" {
		out.Profile.Name = name
	}
	bindings, err := s.repository.ProfileAgentBindingsRecord(ctx, record.ProfileID)
	if err != nil {
		return out, err
	}
	agents := bindings.Agents
	if len(agents) > 0 {
		names := []string{}
		for _, entry := range agents[:min(3, len(agents))] {
			var name, id string
			_ = json.Unmarshal(entry.Name, &name)
			_ = json.Unmarshal(entry.ID, &id)
			if name == "" {
				name = id
			}
			names = append(names, name)
		}
		suffix := ""
		if len(agents) > 3 {
			suffix = msgEtAl
		}
		out.KeptReason = pointer(fmt.Sprintf(msgBoundDevices, strings.Join(names, "、"), suffix))
		out.Guidance = msgBoundGuidance
	} else if len(profile.Routers.Items) > 0 {
		out.KeptReason = pointer(msgKeepRouters)
		out.Guidance = msgKeepRoutersGuidance
	} else {
		owner := s.owners.Installation
		if owner == nil || owner.HasInstallation == nil {
			return out, unavailable()
		}
		exists, err := owner.HasInstallation(ctx, s.repository, record.ProfileID)
		if err != nil {
			return out, err
		}
		if exists {
			out.KeptReason = pointer(msgKeepInstallation)
			out.Guidance = msgKeepInstallationGuidance
		} else {
			defaultID, err := s.repository.DefaultProfileID(ctx)
			if err != nil {
				return out, err
			}
			if defaultID == record.ProfileID {
				out.KeptReason = pointer(msgKeepDefault)
				out.Guidance = msgKeepDefaultGuidance
			}
		}
	}
	out.DeleteProfile = out.KeptReason == nil
	id := "setup_sub_" + record.ID
	if out.DeleteProfile && slices.Contains(record.SubscriptionIDs, id) {
		shared, err := sharedSnapshot(ctx, s.repository)
		if err != nil {
			return out, err
		}
		for _, item := range shared.Subscriptions {
			if item.ID == id {
				out.Subscription = &Choice{id, item.Name}
				break
			}
		}
	}
	return out, nil
}
func (s *Service) abandon(ctx context.Context, record *Record, out *abandonPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.repository.UpdateSystem(ctx, func(commit context.Context, system *model.System) error {
		if err := commit.Err(); err != nil {
			return err
		}
		system.SystemConfig.SetupSuggestionDismissed = pointer(true)
		return commit.Err()
	})
	if err != nil {
		return err
	}
	if out.DeleteProfile {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.repository.DeleteProfile(ctx, record.ProfileID, "")
		if errors.Is(err, repo.ErrInUse) || errors.Is(err, repo.ErrValidation) {
			out.DeleteProfile = false
			out.KeptReason = pointer(err.Error())
			out.Subscription = nil
			out.Guidance = msgKeepRacedGuidance
		} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
			return err
		}
	}
	store := storeFor(s.repository)
	if out.DeleteProfile {
		if out.Subscription != nil {
			id := out.Subscription.ID
			err := updateShared(ctx, s.repository, func(shared *model.Shared) error {
				shared.Subscriptions = slices.DeleteFunc(shared.Subscriptions, func(item model.Subscription) bool { return item.ID == id })
				return nil
			})
			if errors.Is(err, repo.ErrInUse) || errors.Is(err, repo.ErrValidation) {
				out.SubscriptionKept = true
			} else if err != nil {
				return err
			} else {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := refresh.ForgetHealth(ctx, id); err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if _, err := substore.DeleteSubscriptionCache(ctx, id); err != nil {
					return err
				}
			}
		}
		return store.remove(ctx, record.ID)
	}
	record.Abandoned, record.AbandonedAt = pointer(true), pointer(time.Now().UTC().Format(time.RFC3339Nano))
	return store.write(ctx, record)
}
