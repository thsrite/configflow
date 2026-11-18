package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
)

// Service owns onboarding reads and the atomic two-service profile write.
// It retains no context; detached writes are bound to the Runtime generation.
type Service struct {
	Repository *repo.Repository
	DNSLoop    DNSLoopChecker
}

// Operation describes the explicit HTTP operation; HEAD uses Write=false.
type Operation struct {
	ProfileID string
	BaseURL   string
	Write     bool
	Preview   bool
	Body      json.RawMessage
}

// Blocked carries the same review report as preview, with the first blocker.
type Blocked struct{ Report Report }

func (e *Blocked) Error() string {
	if len(e.Report.Blockers) > 0 {
		return e.Report.Blockers[0]
	}
	return msgValidation
}

// Execute runs the read/preview/save contract. Only save detaches from client
// cancellation, and every locally owned commit boundary rechecks shutdown.
func (s *Service) Execute(ctx context.Context, op Operation) (json.RawMessage, error) {
	if op.Write && !op.Preview {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	adopter := currentAdopter()
	raw, revision, err := s.Repository.GetPublishSnapshot(ctx, op.ProfileID, true)
	if err != nil {
		return nil, err
	}
	config, err := fromLegacy(raw)
	if err != nil {
		return nil, err
	}
	if op.Write && !op.Preview && adopter == nil && pendingSetup(ctx, s.Repository, op.ProfileID) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSetupUnavailable
	}
	var before *jsonx.Object
	var beforeNative Document
	modeSource := config
	if op.Write && !op.Preview {
		before, err = s.Repository.GetProfile(ctx, op.ProfileID)
		if err != nil {
			return nil, err
		}
		beforeNative, err = fromLegacy(before)
		if err != nil {
			return nil, err
		}
		modeSource = beforeNative
	}
	mode, err := takeover(ctx, s.Repository, op.ProfileID, modeSource)
	if err != nil {
		if op.Write && !op.Preview && adopter == nil && errors.Is(err, setupflow.ErrReadSession) {
			return nil, ErrSetupUnavailable
		}
		return nil, safeError(err, config)
	}
	options := Options{BaseURL: op.BaseURL, Takeover: mode, DNSLoop: s.DNSLoop}
	if !op.Write {
		state, err := ReadState(config, revision, options)
		if err != nil {
			return nil, safeError(err, config)
		}
		return sanitize(encode(state), config)
	}
	var form Request
	if op.Preview {
		form, err = ParseRequest(op.Body)
		if err != nil {
			return nil, safeError(err, config)
		}
		_, report, err := Prepare(ctx, config, form, options)
		if err != nil {
			return nil, safeError(err, config)
		}
		report.ProfileID = op.ProfileID
		report.Revision = &revision
		return sanitize(encode(report), config)
	}
	var oldRevision model.Int
	if adopter != nil {
		if err = json.Unmarshal(beforeNative["_revision"], &oldRevision); err != nil || oldRevision < 0 || int64(oldRevision) == math.MaxInt64 {
			return nil, invalid(msgRevision)
		}
	}
	beforeMarker, err := markerState(beforeNative)
	if err != nil {
		return nil, err
	}
	var report Report
	var candidate Document
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	saved, revision, err := s.Repository.UpdateProfileTransaction(ctx, op.ProfileID, func(tx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		now, err := fromLegacy(current)
		if err != nil {
			return nil, err
		}
		rev, err := s.Repository.ConfigRevision(tx, op.ProfileID)
		if err != nil {
			return nil, err
		}
		if !revisionMatches(object(op.Body)["expected_config_revision"], rev) {
			return nil, repo.NewInUse(msgStale, nil)
		}
		marker, err := markerState(now)
		if err != nil {
			return nil, err
		}
		gw, err := gatewayConfig(now)
		if err != nil {
			return nil, err
		}
		settings, err := gw.Settings()
		if err != nil {
			return nil, err
		}
		if marker != beforeMarker || mode == "full_tun" && marker == "absent" && settings.Enabled {
			return nil, repo.NewInUse(msgTakeoverChanged, nil)
		}
		form, err = ParseRequest(op.Body)
		if err != nil {
			return nil, safeError(err, now)
		}
		candidate, report, err = Prepare(tx, now, form, options)
		if err != nil {
			return nil, safeError(err, now)
		}
		if !report.Ready {
			data, err := sanitize(encode(report), now)
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(data, &report); err != nil {
				return nil, err
			}
			return nil, &Blocked{report}
		}
		patch, err := legacy(Document{"mihomo": candidate["mihomo"], "oxidns": candidate["oxidns"]})
		if err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		current.Set("mihomo", patch.Value("mihomo"))
		current.Set("oxidns", patch.Value("oxidns"))
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	afterNative, err := fromLegacy(saved)
	if err != nil {
		return nil, err
	}
	var newRevision model.Int
	if adopter != nil {
		if err = json.Unmarshal(afterNative["_revision"], &newRevision); err != nil {
			return nil, err
		}
	}
	if adopter != nil && newRevision == oldRevision+1 {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = adopter(ctx, s.Repository, op.ProfileID, encode(beforeNative), encode(afterNative)); err != nil {
			return nil, err
		}
	}
	repo.ResetConfigContext(ctx)
	report.ProfileID = op.ProfileID
	report.Revision = &revision
	report.Draft = true
	report.PublishGuards = map[string]PublishGuard{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		report.PublishGuards[kind] = PublishGuard{op.ProfileID, kind, revision}
	}
	return sanitize(encode(report), candidate)
}

// IsInputError identifies user-facing form/selection failures for HTTP 400.
func IsInputError(err error) bool { var target *gateway.Error; return errors.As(err, &target) }
