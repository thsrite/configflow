package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func TestFieldComparisonKeepsJSONTypesAndNumbers(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		same                bool
	}{
		{"member-order", `{"z": 1, "a": {"b":true,"a":false}}`, `{"a":{"a":false,"b":true},"z":1}`, true},
		{"large-integer", `9007199254740992`, `9007199254740993`, false},
		{"number-token", `1.0`, `1`, false},
		{"boolean-number", `true`, `1`, false},
		{"null-empty", `null`, `[]`, false},
		{"array-order", `[1,2]`, `[2,1]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			same, err := sameJSONValue(json.RawMessage(tc.before), json.RawMessage(tc.after))
			if err != nil || same != tc.same {
				t.Fatalf("same=%v err=%v, want %v", same, err, tc.same)
			}
		})
	}
}

type storedDNSField struct {
	Owner, Field, Value string
	Position, Revision  int64
}

func storedDNSFields(t *testing.T, s *httpxtest.Server) []storedDNSField {
	t.Helper()
	db, err := sql.Open("sqlite", s.Repo.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT owner, field, value, position, revision FROM fields
		WHERE scope = 'profile' AND field IN ('mosdns', 'oxidns') ORDER BY owner, field`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var fields []storedDNSField
	for rows.Next() {
		var field storedDNSField
		if err := rows.Scan(&field.Owner, &field.Field, &field.Value, &field.Position, &field.Revision); err != nil {
			t.Fatal(err)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestRuleWritesPreserveUntouchedDNSRows(t *testing.T) {
	for _, profile := range []string{"other", "default"} {
		t.Run(profile, func(t *testing.T) {
			s := fixture(t)
			auth := s.Login()
			path := func(suffix string) string {
				return "/api" + suffix + "?profile=" + profile
			}
			steps := []struct {
				name string
				run  func()
			}{
				{"create", func() {
					s.Post(path("/rules"), auth, httpxtest.JSON(map[string]any{"id": "new", "rule_type": "DOMAIN", "value": "new.example", "policy": "DIRECT"})).WantStatus(200)
				}},
				{"update", func() {
					s.Put(path("/rules/new"), auth, httpxtest.JSON(map[string]any{"remark": "changed"})).WantStatus(200)
				}},
				{"policy", func() {
					s.Put(path("/rules/policy"), auth, httpxtest.JSON(map[string]any{"ids": []string{"new"}, "policy": "DIRECT"})).WantStatus(200)
				}},
				{"reorder", func() {
					s.Post(path("/rules/reorder"), auth, httpxtest.JSON(map[string]any{"ids": []string{"new"}, "position": "bottom"})).WantStatus(200)
				}},
				{"delete", func() { s.Delete(path("/rules/new"), auth).WantStatus(200) }},
				{"create-set", func() {
					s.Post(path("/rule-sets"), auth, httpxtest.JSON(map[string]any{"id": "new-set", "library_rule_id": "ads", "policy": "DIRECT"})).WantStatus(200)
				}},
				{"delete-set", func() { s.Delete(path("/rule-sets/new-set"), auth).WantStatus(200) }},
			}
			for _, step := range steps {
				before := storedDNSFields(t, s)
				step.run()
				after := storedDNSFields(t, s)
				if !reflect.DeepEqual(before, after) {
					for i, row := range before {
						if row != after[i] {
							t.Fatalf("first unchanged DNS row mutation at %s:\nbefore=%+v\nafter=%+v", step.name, row, after[i])
						}
					}
				}
			}
			for _, route := range []string{"/rules/missing", "/rule-sets/missing"} {
				before, err := s.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				s.Delete(path(route), auth).WantStatus(404)
				after, err := s.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if before != after {
					t.Fatalf("404 changed stored rows: %s", route)
				}
			}
		})
	}
}

func TestRuleDeleteChangesOnlyReferencedDNSRow(t *testing.T) {
	s := fixture(t)
	_, err := s.Repo.UpdateProfile(s.Context(), "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mosdns", json.RawMessage(`{"direct_rules":["rule_1"]}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before := storedDNSFields(t, s)
	s.Delete("/api/rules/rule_1?profile=other", s.Login()).WantStatus(200)
	after := storedDNSFields(t, s)
	for i, row := range before {
		if row.Owner == "other" && row.Field == "mosdns" {
			if row.Value == after[i].Value || row.Revision >= after[i].Revision || row.Position != after[i].Position {
				t.Fatalf("referenced DNS row was not updated: before=%+v after=%+v", row, after[i])
			}
			continue
		}
		if row != after[i] {
			t.Fatalf("unreferenced row changed: before=%+v after=%+v", row, after[i])
		}
	}
}
