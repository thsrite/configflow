package refresh

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/weburl"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

func notFalse(value *bool) bool { return value == nil || *value }

func sourceHost(address string) string {
	parts, err := weburl.Parse(address)
	if err != nil {
		return ""
	}
	return weburl.Hostname(parts)
}

// SubscriptionTasks projects native task rows from the shared source snapshot.
func (s *Service) SubscriptionTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	r, err := s.repository()
	if err != nil {
		return nil, err
	}
	type subView struct {
		id       string
		name     string
		url      string
		enabled  bool
		interval model.Int
	}
	var subs []subView
	if err := r.View(ctx, func(document *model.Document) error {
		subs = make([]subView, 0, len(document.Shared.Subscriptions))
		for _, sub := range document.Shared.Subscriptions {
			subs = append(subs, subView{
				id:       sub.ID,
				name:     sub.Name,
				url:      sub.URL,
				enabled:  notFalse(sub.Enabled),
				interval: SourceInterval(sub),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rows := make([]*tasks.Task, 0, len(subs))
	for _, sub := range subs {
		key := SourceTaskKey(sub.id)
		entry := statuses.Entry(key)
		interval := sub.interval
		hasURL := substore.SourceURLConfigured(sub.url)
		enabled := sub.enabled && hasURL
		var due *time.Time
		if enabled {
			baseline, err := s.sourceCacheAtID(ctx, sub.id, sub.url)
			if err != nil {
				return nil, err
			}
			due, err = s.nextDue(entry, interval, baseline, nil)
			if err != nil {
				return nil, err
			}
		}
		history, err := tasks.ProjectHistory(entry)
		if err != nil {
			return nil, err
		}
		name := sub.name
		if name == "" {
			name = sub.id
		}
		hint, message := "", ""
		if sub.url == "" {
			hint = msgNoURL
		} else if !hasURL {
			hint = msgInvalidSourceURL
		}
		if !sub.enabled {
			message = msgSubscriptionDisabled
		}
		seconds, _ := json.Marshal(interval)
		link := "/subscriptions"
		rows = append(rows, &tasks.Task{Key: key, Category: "subscription", Name: name, ResourceID: sub.id, Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: hasURL, CanRun: hasURL, RunHint: hint, Running: IsRunning(key), NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: &link, NotificationsEnabled: &enabled, HistoryData: history})
	}
	return rows, nil
}

func (s *Service) runTask(ctx context.Context, key, id string) error {
	category, _, _ := strings.Cut(key, ":")
	_, err := s.RunResourceNow(ctx, category, id)
	var busy *TaskBusy
	var lookup *NotFoundError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &busy):
		return &tasks.Busy{Message: busy.Message}
	case errors.As(err, &lookup):
		return &tasks.NotFound{Message: lookup.Message}
	case errors.Is(err, repo.ErrValidation):
		return &tasks.Invalid{Message: err.Error()}
	}
	return err
}

// MaxPause bounds the pause of the scheduled-refresh loop between due times.
const MaxPause = 10 * time.Minute

// refreshPacer paces scheduled-refresh by the next due time (design
// o17-runtime-state.md 2.6): the loop sleeps until the earliest resource is
// due (at most MaxPause) and is woken by configuration commits (intervals,
// new sources) and by finished refreshes, which move the next due time.
type refreshPacer struct {
	wake    lifecycle.Signal
	pause   atomic.Int64 // time.Duration
	commits repo.CommitWake
}

// pace follows the repository's configuration commits and sets the pause to
// the next due time.
func (s *Service) pace(ctx context.Context) {
	r, err := s.repository()
	if err != nil {
		return
	}
	s.loop.commits.Follow(r, repo.ChangeConfig, s.loop.wake.Notify)
	now := s.nowUTC()
	jobs, next, err := s.dueAndNext(ctx, &now)
	if err != nil {
		return
	}
	pause := MaxPause
	if len(jobs) > 0 {
		pause = TickSeconds * time.Second
	}
	if next != nil {
		pause = min(pause, max(next.Sub(now), time.Second))
	}
	s.loop.pause.Store(int64(pause))
}
