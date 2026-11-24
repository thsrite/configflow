package discovery

import (
	"context"
	"errors"
	"slices"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

// AutoOutcome counts the work completed in one automatic round.
type AutoOutcome struct {
	Probed    int `json:"probed"`
	Applied   int `json:"applied"`
	Rechecked int `json:"rechecked"`
}

func (svc *Service) autoRound(ctx context.Context, id string) (AutoOutcome, error) {
	var out AutoOutcome
	c, err := svc.loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	s := c.Settings
	if !s.AutoProbe || proxyPath(c) == nil {
		return out, nil
	}
	as, err := svc.profileAgents(ctx, id)
	if err != nil {
		return out, err
	}
	if len(capable(as, minProbeVersion)) == 0 {
		return out, nil
	}
	listing, err := svc.LoadDomains(ctx, id, 7, "uncovered", "")
	if err != nil {
		return out, err
	}
	stale := now().AddDate(0, 0, -3).Format("2006-01-02T15:04:05")
	values := []string{}
	for _, d := range listing.Items {
		if !slices.Contains(d.Tags, "uncovered") {
			continue
		}
		needed := false
		for _, h := range d.Hosts[:min(3, len(d.Hosts))] {
			needed = needed || h.Probe == nil || h.Probe.CheckedAt < stale
		}
		if needed {
			values = append(values, d.Domain)
		}
		if len(values) >= 20 {
			break
		}
	}
	out.Probed = len(values)
	if len(values) > 0 {
		if autoStopping(ctx) {
			return out, nil
		}
		_, _, err = svc.startJob(ctx, id, "probe", true, func(ctx context.Context, p func(int, int)) (outcome, error) { return svc.runProbes(ctx, id, values, p) })
		if err != nil {
			return out, err
		}
		if s.AutoApply && !autoStopping(ctx) {
			refreshed, err := svc.LoadDomains(ctx, id, 7, "uncovered", "")
			if err != nil {
				return out, err
			}
			entries := []ApplyEntry{}
			evidence := map[string]Probe{}
			for _, d := range refreshed.Items {
				v := d.Suggestion
				if !slices.Contains(values, d.Domain) || v == nil || v.Target == nil || v.Confidence < s.Confidence {
					continue
				}
				entries = append(entries, ApplyEntry{d.Domain, "DOMAIN-SUFFIX", *v.Target})
				evidence[d.Domain] = Probe{Verdict: v.Verdict, Confidence: v.Confidence}
			}
			if len(entries) > 0 {
				result, err := svc.ApplyDomains(ctx, id, entries, "auto", evidence)
				if err != nil {
					if !errors.Is(err, repo.ErrValidation) {
						return out, err
					}
					logger.Warning("Auto apply skipped: " + err.Error())
				} else {
					out.Applied = len(result.Added)
				}
			}
		}
	}
	c, err = svc.loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	due := now().AddDate(0, 0, -30).Format("2006-01-02T15:04:05")
	history := []HistoryEntry{}
	for _, h := range c.Settings.History {
		if (h.Target == "direct" || h.Target == "proxy") && h.AppliedAt < due && (h.Recheck == nil || h.Recheck.At < due) {
			history = append(history, h)
		}
		if len(history) >= 10 {
			break
		}
	}
	out.Rechecked = len(history)
	if len(history) > 0 {
		if autoStopping(ctx) {
			return out, nil
		}
		values := []string{}
		for _, h := range history {
			values = append(values, h.Value)
		}
		_, results, err := svc.startJob(ctx, id, "probe", true, func(ctx context.Context, p func(int, int)) (outcome, error) { return svc.runProbes(ctx, id, values, p) })
		if err != nil {
			return out, err
		}
		checks := map[[2]string]Recheck{}
		at := timestamp()
		for _, h := range history {
			p, found := results[h.Value]
			if !found {
				for _, host := range sortedKeys(results) {
					if stringsHasDomain(host, h.Value) {
						p = results[host]
						found = true
						break
					}
				}
			}
			check := Recheck{At: at, Reason: msgRecheckEmpty}
			if found {
				check.Verdict = new(p.Verdict)
				check.Reason = ""
				switch {
				case p.Verdict == "unreachable":
					check.SuggestRemove = true
					check.Reason = msgRecheckGone
				case h.Target == "proxy" && p.Target != nil && *p.Target == "direct" && (p.Verdict == "direct_only" || p.Verdict == "both_ok"):
					check.SuggestRemove = true
					check.Reason = msgRecheckDirect
				case h.Target == "direct" && p.Verdict == "needs_proxy":
					check.SuggestRemove = true
					check.Reason = msgRecheckProxy
				}
			}
			checks[[2]string{h.Value, h.Target}] = check
		}
		err = svc.editSettings(ctx, id, func(s *Settings) error {
			for i, h := range s.History {
				if v, ok := checks[[2]string{h.Value, h.Target}]; ok {
					s.History[i].Recheck = new(v)
				}
			}
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// RunAutoTask records a scheduled or manually triggered automatic round.
func (s *Service) RunAutoTask(ctx context.Context, id, trigger string) (*AutoOutcome, error) {
	ctx = lifecycle.Detach(ctx)
	c, err := s.loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	if !c.Settings.AutoProbe {
		return nil, nil
	}
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	var store *taskstatus.Store
	if s.Notifications != nil {
		store = s.Notifications.TaskStatus(ctx, r.DataDir())
	} else {
		store = taskstatus.New(r.DataDir())
	}
	started, clock := taskstatus.NowISO(), time.Now()
	record := func(ok, skipped bool, msg string) error {
		_, err := store.SaveResult(ctx, "domain_discovery:"+id, taskstatus.Result{OK: ok, Skipped: skipped, Error: msg, StartedAt: started, DurationMs: float64(time.Since(clock).Milliseconds()), Trigger: trigger})
		return err
	}
	if proxyPath(c) == nil {
		return nil, record(false, true, msgAutoPath)
	}
	as, err := s.profileAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(capable(as, minProbeVersion)) == 0 {
		return nil, record(false, true, msgAutoAgents)
	}
	out, err := s.autoRound(ctx, id)
	if err != nil {
		var busy *Busy
		if errors.As(err, &busy) {
			return nil, record(false, true, msgAutoBusy)
		}
		if recErr := record(false, false, err.Error()); recErr != nil {
			logger.Exception("Record domain task failed", recErr)
		}
		return nil, err
	}
	return &out, record(true, false, "")
}
func stringsHasDomain(host, value string) bool {
	return len(host) > len(value) && host[len(host)-len(value)-1:] == "."+value
}
func (s *Service) autoPeriodic() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name: "domain-discovery-auto", Start: lifecycle.AtBoot, FirstDelay: 120 * time.Second,
		Interval: func(context.Context) time.Duration { return 600 * time.Second },
		OnStart:  s.activateMatcherCache,
		Round:    s.automaticRound,
	}
}

func (s *Service) automaticRound(ctx context.Context) error {
	if _, err := s.repo(); err != nil {
		return err
	}
	profiles, err := s.listProfiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		select {
		case <-lifecycle.Stopping(ctx):
			return nil
		default:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		func() {
			scope := repo.WithScope(lifecycle.Detach(ctx))
			defer repo.ResetConfigContext(scope)
			defer func() {
				if p := recover(); p != nil {
					logger.Errorf("Domain discovery automatic round failed: %v", p)
				}
			}()
			if _, err := s.RunAutoTask(scope, p.ID, "schedule"); err != nil {
				logger.Exception("Domain discovery automatic round failed", err)
			}
		}()
	}
	return nil
}

func autoStopping(ctx context.Context) bool {
	select {
	case <-lifecycle.Stopping(ctx):
		return true
	default:
		return ctx.Err() != nil
	}
}
