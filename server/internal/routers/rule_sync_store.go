package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

// RuleSyncBaseline pins source versions and complete device ownership proofs.
type RuleSyncBaseline struct {
	Control         string                     `json:"control"`
	Rules           map[string]*RuleSyncRecord `json:"rules"`
	Desired         *Desired                   `json:"desired"`
	StaticResources []ROSResource              `json:"static_resources"`
	Policy          json.RawMessage            `json:"policy"`
	NATList         *NATBaseline               `json:"nat_list"`
}

// RuleSyncLog is one successful destination delta.
type RuleSyncLog struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// RuleSyncReceipt is runtime state outside configuration revisions and exports.
type RuleSyncReceipt struct {
	Status    string                     `json:"status,omitempty"`
	Message   string                     `json:"message,omitempty"`
	CheckedAt *string                    `json:"checked_at,omitempty"`
	SyncedAt  *string                    `json:"synced_at,omitempty"`
	UpdatedAt *string                    `json:"updated_at,omitempty"`
	Baseline  *RuleSyncBaseline          `json:"baseline,omitempty"`
	Rules     map[string]*RuleSyncRecord `json:"rules,omitempty"`
	Added     int                        `json:"added,omitempty"`
	Removed   int                        `json:"removed,omitempty"`
	Log       []RuleSyncLog              `json:"log,omitempty"`
}

// RuleSyncPublicRule is the bounded credential-free rule projection.
type RuleSyncPublicRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SourceType    string `json:"source_type"`
	IPCount       int    `json:"ip_count"`
	DomainCount   int    `json:"domain_count"`
	IgnoredCount  int    `json:"ignored_count"`
	InvalidCount  int    `json:"invalid_count"`
	Version       string `json:"version"`
	SyncedVersion string `json:"synced_version"`
}

// RuleSyncStatus is the GET/run response. It contains neither content nor ownership data.
type RuleSyncStatus struct {
	Enabled   bool                 `json:"enabled"`
	Status    string               `json:"status"`
	Message   string               `json:"message"`
	CheckedAt *string              `json:"checked_at"`
	SyncedAt  *string              `json:"synced_at"`
	UpdatedAt *string              `json:"updated_at"`
	Added     int                  `json:"added"`
	Removed   int                  `json:"removed"`
	Ready     bool                 `json:"ready"`
	Log       []RuleSyncLog        `json:"log"`
	Rules     []RuleSyncPublicRule `json:"rules"`
}

// Public projects a receipt without exposing its private baseline.
func (r RuleSyncReceipt) Public(enabled bool) RuleSyncStatus {
	out := RuleSyncStatus{Enabled: enabled, Status: orText(r.Status, "waiting"), Message: orText(r.Message, messageSyncBaseline), CheckedAt: r.CheckedAt, SyncedAt: r.SyncedAt, UpdatedAt: r.UpdatedAt, Added: r.Added, Removed: r.Removed, Ready: r.Baseline != nil, Log: append([]RuleSyncLog{}, r.Log[max(0, len(r.Log)-20):]...), Rules: []RuleSyncPublicRule{}}
	rules := r.Rules
	if len(rules) == 0 && r.Baseline != nil {
		rules = r.Baseline.Rules
	}
	for _, id := range sortedSyncKeys(rules) {
		v := rules[id]
		if v == nil {
			continue
		}
		synced := ""
		if r.Baseline != nil && r.Baseline.Rules[id] != nil {
			synced = r.Baseline.Rules[id].Version
		}
		out.Rules = append(out.Rules, RuleSyncPublicRule{id, v.Name, v.SourceType, v.IPCount, v.DomainCount, v.IgnoredCount, v.InvalidCount, v.Version[:min(12, len(v.Version))], synced[:min(12, len(synced))]})
	}
	return out
}

// PausedMessage explains both why the job paused and how to resume it.
func PausedMessage(r RuleSyncReceipt) string {
	reason := strings.TrimRight(r.Message, "。.；;，, ")
	if reason == "" {
		return messageSyncPaused + "。" + messageSyncResume
	}
	result := messageSyncPaused + "：" + reason
	if !strings.Contains(reason, "预览发布") {
		result += "。" + messageSyncResume
	}
	return result
}

var syncReceiptLocks = lockx.NewLeafKeyed[string]("router-sync-receipt")

// RuleSyncStore persists atomic receipts beneath one repository data directory.
type RuleSyncStore struct{ DataDir string }

func (s RuleSyncStore) path(profile, id string) string {
	return filepath.Join(s.DataDir, "router-rule-sync", routeDigest([]string{profile, id})+".json")
}

// Read treats corrupt or unreadable receipts as paused; an absent receipt is waiting.
func (s RuleSyncStore) Read(ctx context.Context, profile, id string) (RuleSyncReceipt, error) {
	if err := ctx.Err(); err != nil {
		return RuleSyncReceipt{}, err
	}
	data, err := os.ReadFile(s.path(profile, id))
	if errors.Is(err, os.ErrNotExist) {
		return RuleSyncReceipt{}, nil
	}
	var result RuleSyncReceipt
	if err != nil || json.Unmarshal(data, &result) != nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return RuleSyncReceipt{Status: "paused", Message: messageSyncUnreadable}, nil
	}
	return result, nil
}

// Write checks shutdown after taking the shared leaf lock and before atomic replacement.
func (s RuleSyncStore) Write(ctx context.Context, profile, id string, value RuleSyncReceipt) error {
	path := s.path(profile, id)
	_, release, err := syncReceiptLocks.Lock(ctx, path, 0)
	if err != nil {
		return err
	}
	defer release()
	if err = ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode rule sync receipt: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create rule sync directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".receipt-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write rule sync receipt: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("replace rule sync receipt: %w", err)
	}
	return nil
}

// NeedsBaseline reports whether enabled auto-sync needs a fresh manual publication.
func (s RuleSyncStore) NeedsBaseline(ctx context.Context, profile string, r *Router) (bool, error) {
	if r.Type != "routeros" || !r.AutoRuleSync {
		return false, nil
	}
	v, err := s.Read(ctx, profile, r.ID)
	return v.Baseline == nil || v.Status == "paused" || v.Status == "failed" || v.Status == "waiting", err
}
func syncNow(ctx context.Context) string {
	return lifecycle.ClockFrom(ctx).Now().UTC().Format("2006-01-02T15:04:05+00:00")
}
