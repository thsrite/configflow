package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// TestO10SaveSystemMemberMatchesSaveSystemRecord replays the settings handler
// edit (load, change one member, optional concurrent commit, save with the read
// baseline) through the former whole-record path and the single-member path and
// requires identical stored system records.
func TestO10SaveSystemMemberMatchesSaveSystemRecord(t *testing.T) {
	ctx := context.Background()
	agents := `[{"id":"agent-a","name":"A","service_type":"mihomo","profile_id":"default","token":"synthetic-agent-token","deployments":{"d1":{"id":"d1"}}}]`
	// Stored null or wrong-typed system_config/backup is rejected by validation
	// ("系统设置和备份设置必须是对象"), so only object members are replayed.
	tests := []struct {
		name       string
		member     string // system_config or backup
		stored     string // stored member JSON; "" leaves the default
		key        string // system_config member; backup cases edit webdav_username
		value      string
		concurrent func(*model.System) error
	}{
		{name: "object setting", member: "system_config", stored: `{"server_domain":"https://a.example.com","future":{"x":1}}`, key: "config_token", value: `"synthetic-new"`},
		{name: "default setting", member: "system_config", key: "server_domain", value: `"https://b.example.com"`},
		{name: "concurrent sibling", member: "system_config", stored: `{"server_domain":"https://e.example.com"}`, key: "config_token", value: `"synthetic-token"`,
			concurrent: func(s *model.System) error {
				s.SystemConfig.ServerDomain = "https://concurrent.example.com"
				s.Agents[0].Name = "renamed"
				return nil
			}},
		{name: "concurrent same key", member: "system_config", key: "server_domain", value: `"https://mine.example.com"`,
			concurrent: func(s *model.System) error { s.SystemConfig.ServerDomain = "https://theirs.example.com"; return nil }},
		{name: "object backup", member: "backup", stored: `{"webdav_url":"https://dav.example.com","future":true}`, key: "webdav_username", value: `"synthetic-user"`},
		{name: "concurrent backup", member: "backup", key: "webdav_username", value: `"synthetic-user"`,
			concurrent: func(s *model.System) error { path := "/other/"; s.Backup.WebDAVPath = &path; return nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			open := func() *repo.Repository {
				t.Helper()
				r, err := repo.OpenDefault(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
					if err := s.SetJSON("agents", json.RawMessage(agents)); err != nil {
						return err
					}
					if tt.stored != "" {
						return s.SetJSON(tt.member, json.RawMessage(tt.stored))
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return r
			}
			commitConcurrent := func(r *repo.Repository) {
				t.Helper()
				if tt.concurrent == nil {
					return
				}
				if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error { return tt.concurrent(s) }); err != nil {
					t.Fatal(err)
				}
			}
			stored := func(r *repo.Repository) json.RawMessage {
				t.Helper()
				system, err := r.System(ctx)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := model.Marshal(system)
				if err != nil {
					t.Fatal(err)
				}
				// rule_proxy_token is generated per repository; compare the rest.
				var tree map[string]any
				if err := json.Unmarshal(raw, &tree); err != nil {
					t.Fatal(err)
				}
				if settings, ok := tree["system_config"].(map[string]any); ok {
					delete(settings, "rule_proxy_token")
				}
				raw, err = json.Marshal(tree)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}

			// Former path: whole System snapshot and baseline.
			former := open()
			system, err := former.System(ctx)
			if err != nil {
				t.Fatal(err)
			}
			base := *system
			if tt.member == "system_config" {
				err = system.SystemConfig.SetJSON(tt.key, json.RawMessage(tt.value))
			} else {
				err = json.Unmarshal(json.RawMessage(tt.value), &system.Backup.WebDAVUsername)
			}
			if err != nil {
				t.Fatal(err)
			}
			commitConcurrent(former)
			formerErr := func() error { _, err := former.SaveSystemRecord(ctx, system, &base); return err }()

			// Single-member path.
			narrow := open()
			var narrowErr error
			if tt.member == "system_config" {
				settings, err := narrow.SystemConfig(ctx)
				if err != nil {
					t.Fatal(err)
				}
				baseline := *settings
				if err := settings.SetJSON(tt.key, json.RawMessage(tt.value)); err != nil {
					t.Fatal(err)
				}
				commitConcurrent(narrow)
				_, narrowErr = narrow.SaveSystemConfig(ctx, settings, &baseline)
			} else {
				backup, err := narrow.Backup(ctx)
				if err != nil {
					t.Fatal(err)
				}
				baseline := *backup
				if err := json.Unmarshal(json.RawMessage(tt.value), &backup.WebDAVUsername); err != nil {
					t.Fatal(err)
				}
				commitConcurrent(narrow)
				_, narrowErr = narrow.SaveBackup(ctx, backup, &baseline)
			}
			if (formerErr == nil) != (narrowErr == nil) {
				t.Fatalf("errors differ: former=%v narrow=%v", formerErr, narrowErr)
			}
			want, got := stored(former), stored(narrow)
			if !testjson.Equal(want, got) {
				t.Fatalf("stored system differs:\n%s", testjson.Diff(want, got))
			}
		})
	}
}
