package refresh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/jsonstore"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// The three directories searched for URL sources (current and legacy names).
var ruleDirs = [...]string{"", "by-source", "by-id"}

type ruleFiles struct {
	root string
	dirs [len(ruleDirs)]jsonstore.Signature
}

func ruleDirectoryState(root string) (ruleFiles, bool) {
	state := ruleFiles{root: root}
	if root == "" {
		return state, false
	}
	for i, dir := range ruleDirs {
		sig, err := jsonstore.FileSignature(filepath.Join(root, dir))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return state, false
		}
		state.dirs[i] = sig
	}
	return state, true
}

// A symlink target can appear or move without changing the link's directory.
func (f ruleFiles) plain() bool {
	for i, dir := range ruleDirs {
		if f.dirs[i] == (jsonstore.Signature{}) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(f.root, dir))
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return false
			}
		}
	}
	return true
}

type ruleRow struct {
	task    tasks.Task
	receipt string
	dueNow  bool
}
type ruleRows struct {
	head  store.Head
	rows  []ruleRow
	files *ruleFiles
	// Directory timestamps cover replacement, creation and removal. Files
	// whose mtimes actually contribute to a row also need an in-place check.
	stamps map[string]jsonstore.Signature
}

func (c *ruleRows) current(r *repo.Repository, head *store.Head, statuses taskstatus.Snapshot) bool {
	if c == nil || head == nil || !head.SameConfig(c.head) {
		return false
	}
	for _, row := range c.rows {
		if string(statuses[row.task.Key]) != row.receipt {
			return false
		}
	}
	if c.files != nil {
		root, err := r.SharedRulesDir()
		if err != nil {
			return false
		}
		files, ok := ruleDirectoryState(root)
		if !ok || files != *c.files {
			return false
		}
		for path, sig := range c.stamps {
			if actual, err := jsonstore.FileSignature(path); err != nil || actual != sig {
				return false
			}
		}
	}
	return true
}

// cacheTime records exactly the directory and file evidence used by the row.
func (c *ruleRows) cacheTime(r *repo.Repository, doc *model.Document, rule model.RuleLibraryItem) (*string, bool, error) {
	stable := true
	if c.files == nil {
		root, err := r.SharedRulesDir()
		if err != nil && !errors.Is(err, repo.ErrRejected) && !isFileError(err) {
			return nil, false, err
		}
		files, ok := ruleDirectoryState(root)
		c.files = &files
		stable = err == nil && ok && files.plain()
	}
	if c.files.root == "" {
		return nil, false, nil
	}
	path := rules.StoredCacheReadPath(rules.LibraryRule(rule), nil, doc, c.files.root)
	if path == "" {
		return nil, stable, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, stable && errors.Is(err, os.ErrNotExist), nil
	}
	c.stamps[path] = jsonstore.InfoSignature(info)
	if !info.Mode().IsRegular() {
		return nil, stable, nil
	}
	return ruleTimestamp(info), stable, nil
}

func copyField[T any](p *T) *T {
	if p == nil {
		return nil
	}
	return new(*p)
}

func (s *Service) taskCopies(c *ruleRows) []*tasks.Task {
	rows := make([]*tasks.Task, 0, len(c.rows))
	for _, item := range c.rows {
		row := item.task
		row.Running = IsRunning(row.Key)
		row.Link = copyField(row.Link)
		row.NotificationsEnabled = copyField(row.NotificationsEnabled)
		row.IntervalSeconds = bytes.Clone(row.IntervalSeconds)
		if item.dueNow {
			now := s.nowUTC()
			row.NextRunAt = tasks.Timestamp(&now)
		} else {
			row.NextRunAt = copyField(row.NextRunAt)
		}
		h := *row.HistoryData
		h.LastRunAt, h.LastStatus, h.LastError = copyField(h.LastRunAt), copyField(h.LastStatus), copyField(h.LastError)
		h.LastDurationMs, h.ConsecutiveFailures = bytes.Clone(h.LastDurationMs), bytes.Clone(h.ConsecutiveFailures)
		h.LastTrigger, h.LastSuccessAt = copyField(h.LastTrigger), copyField(h.LastSuccessAt)
		h.CacheRetained, h.CacheUpdatedAt = copyField(h.CacheRetained), copyField(h.CacheUpdatedAt)
		row.HistoryData = &h
		rows = append(rows, &row)
	}
	return rows
}

// RuleTasks projects URL source rows, evaluating clock and job state on every call.
func (s *Service) RuleTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	r, err := s.repository()
	if err != nil {
		return nil, err
	}
	var result *ruleRows
	err = r.ViewAt(ctx, func(doc *model.Document, head *store.Head) error {
		s.ruleRowsMu.Lock()
		defer s.ruleRowsMu.Unlock()
		if s.ruleRows.current(r, head, statuses) {
			result = s.ruleRows
			return nil
		}
		result = &ruleRows{stamps: make(map[string]jsonstore.Signature)}
		stable := head != nil
		for _, rule := range doc.Shared.RuleLibrary {
			if err := ctx.Err(); err != nil {
				return err
			}
			url := textOr(rule.URL, "")
			if !urlRule(rule) || url == "" {
				continue
			}
			key := RuleTaskKey(rule.ID)
			entry := statuses.Entry(key)
			interval := RemoteInterval(rule)
			enabled := notFalse(rule.Enabled)
			_, hasTime := parseStamp(textOr(entry.LastRunAt, ""))
			var baseline *string
			if textOr(entry.CacheUpdatedAt, "") == "" || enabled && interval != 0 && !hasTime {
				var ok bool
				baseline, ok, err = result.cacheTime(r, doc, rule)
				if err != nil {
					return err
				}
				stable = stable && ok
			}
			var due *time.Time
			dueNow := enabled && interval != 0 && !hasTime && baseline == nil
			if enabled && !dueNow {
				due, err = s.nextDue(entry, interval, baseline, nil)
				if err != nil {
					return err
				}
			}
			history, err := tasks.ProjectHistory(entry)
			if err != nil {
				return err
			}
			if textOr(history.CacheUpdatedAt, "") == "" {
				history.CacheUpdatedAt = baseline
			}
			name := rule.Name
			if name == "" {
				name = rule.ID
			}
			message := ""
			if !enabled {
				message = msgRuleDisabled
			}
			seconds, _ := json.Marshal(interval)
			row := tasks.Task{
				Key: key, Category: "rule_source", Name: name, ResourceID: rule.ID, Detail: sourceHost(url),
				Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: true, CanRun: true,
				NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: new("/rule-library"),
				NotificationsEnabled: &enabled, HistoryData: history,
			}
			result.rows = append(result.rows, ruleRow{row, string(statuses[key]), dueNow})
		}
		if stable {
			result.head = *head
			if result.current(r, head, statuses) {
				s.ruleRows = result
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.taskCopies(result), nil
}
