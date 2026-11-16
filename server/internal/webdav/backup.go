package webdav

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"configflow/internal/logx"
)

var logger = logx.Get("backend.utils.webdav_backup")
var backupName = regexp.MustCompile(`^config_backup_([0-9]{8})_([0-9]{6})(?:_([0-9]{6}))?\.json$`)

// Result identifies the uploaded backup and successful retention deletions.
type Result struct {
	Filename string
	Removed  int
}

// ExpiredBackups returns only program-owned names outside both retention windows,
// newest first. Zero latest retention keeps every file, including old backups.
func ExpiredBackups(names []string, latest, daily int, today time.Time) []string {
	result := []string{}
	if latest <= 0 {
		return result
	}
	type backup struct{ name, day, clock, micro string }
	backups := []backup{}
	for _, name := range names {
		parts := backupName.FindStringSubmatch(name)
		if parts != nil {
			backups = append(backups, backup{name, parts[1], parts[2], parts[3]})
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		a, b := backups[i], backups[j]
		if a.day != b.day {
			return a.day > b.day
		}
		if a.clock != b.clock {
			return a.clock > b.clock
		}
		if a.micro != b.micro {
			return a.micro > b.micro
		}
		return a.name > b.name
	})
	keep := map[string]bool{}
	for _, item := range backups[:min(latest, len(backups))] {
		keep[item.name] = true
	}
	if daily > 0 {
		first := today.AddDate(0, 0, -daily+1).Format("20060102")
		seen := map[string]bool{}
		for _, item := range backups {
			if item.day >= first && !seen[item.day] {
				seen[item.day] = true
				keep[item.name] = true
			}
		}
	}
	for _, item := range backups {
		if !keep[item.name] {
			result = append(result, item.name)
		}
	}
	return result
}

// Upload sends an in-memory JSON snapshot and cleans old backups only after PUT
// succeeds. Remote cleanup failures never invalidate a successful new backup.
func (s *Service) Upload(ctx context.Context, snapshot json.RawMessage, settings Settings, automatic bool) (Result, error) {
	ctx = context.WithoutCancel(ctx)
	c, err := s.client(settings)
	if err != nil {
		return Result{}, err
	}
	directory := "/" + strings.Trim(settings.Path, "/") + "/"
	if directory == "//" {
		directory = "/"
	}
	if err := c.ensure(ctx, directory); err != nil {
		return Result{}, err
	}
	if !json.Valid(snapshot) {
		return Result{}, fmt.Errorf("backup snapshot is not valid JSON")
	}
	filename := ""
	// Go's layout uses fractional seconds only with a dot; format microseconds
	// explicitly to retain the existing filename protocol without a temporary file.
	stamp := s.Now()
	filename = "config_backup_" + stamp.Format("20060102_150405") + fmt.Sprintf("_%06d.json", stamp.Nanosecond()/1000)
	if err := c.put(ctx, directory+filename, snapshot); err != nil {
		return Result{}, err
	}
	trigger := msgManual
	if automatic {
		trigger = msgAutomatic
	}
	logger.Infof(msgUploaded, trigger, c.safePath(directory), filename, float64(len(snapshot))/1024)
	removed := 0
	if settings.KeepLatest > 0 {
		names, err := c.list(ctx, directory)
		if err != nil {
			logger.Warningf(msgListFailed, DescribeError(err))
		} else {
			for _, name := range ExpiredBackups(names, settings.KeepLatest, settings.KeepDaily, s.Now()) {
				if err := c.remove(ctx, directory+name); err != nil {
					logger.Warningf(msgDeleteFailed, name, DescribeError(err))
				} else {
					removed++
				}
			}
		}
	}
	if removed > 0 {
		logger.Infof(msgPruned, removed, settings.KeepLatest, settings.KeepDaily)
	}
	return Result{filename, removed}, nil
}
