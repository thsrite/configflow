package setup

import (
	"configflow/internal/model"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeDigestsIgnoreObjectOrderAndKeepValues(t *testing.T) {
	a := json.RawMessage(`{"proxy_groups":[],"mihomo":{"custom_config":"secret: synthetic\n","unknown":{"z":12345678901234567890,"a":"中文"}},"rule_configs":[]}`)
	b := json.RawMessage(`{"rule_configs":[],"mihomo":{"unknown":{"a":"中文","z":12345678901234567890},"custom_config":"secret: synthetic\n"},"proxy_groups":[]}`)
	for _, hash := range []func(json.RawMessage) (string, error){CoreFingerprint, func(raw json.RawMessage) (string, error) { return OwnedDigest(raw, false) }} {
		x, err := hash(a)
		if err != nil {
			t.Fatal(err)
		}
		y, err := hash(b)
		if err != nil || x != y {
			t.Fatal("map order changed fingerprint", err)
		}
		changed := json.RawMessage(strings.Replace(string(b), "12345678901234567890", "12345678901234567891", 1))
		z, err := hash(changed)
		if err != nil || z == x {
			t.Fatal("large integer change ignored", err)
		}
	}
}

func TestOldWizardDigestConflictCanRecover(t *testing.T) {
	s, ctx := preparedFixture(t, "gateway")
	s.owners.Installation = installedOwner()
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query"}`)
	if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	store := storeFor(s.repository)
	record, err := store.read(ctx, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	record.DigestVersion = nil
	if err = record.clear("digest_version"); err != nil {
		t.Fatal(err)
	}
	if err = store.write(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, testSessionID); err != nil {
		t.Fatal("legacy session cannot be read", err)
	}
	if _, err = s.Prepare(ctx, testSessionID, body, "http://localhost"); err == nil || err.Error() != msgForeignEdit {
		t.Fatal("legacy hash did not fail closed", err)
	}
	if _, err = s.Action(ctx, testSessionID, "abandon", json.RawMessage(`{}`), ""); err != nil {
		t.Fatal("conflicted session cannot be abandoned", err)
	}
	if _, err = s.Create(ctx, json.RawMessage(`{"id":"`+testSessionID+`","goal":"gateway","name":"Synthetic wizard"}`)); err != nil {
		t.Fatal("conflicted session cannot be restarted", err)
	}
	if _, err = s.Get(ctx, testSessionID); err != nil {
		t.Fatal("restarted session cannot be resumed", err)
	}
}

func TestWizardCreatesStringCredentialFromInvalidStoredScalar(t *testing.T) {
	r, ctx := setupRepository(t)
	// Writers refuse a credential that is not text (O18 T1c); the store holds
	// it as a damaged database would.
	db, err := sql.Open("sqlite", r.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE fields SET value='{"config_token":123}', revision=revision+1 WHERE scope='system' AND owner='' AND field='system_config'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Create(ctx, json.RawMessage(`{"id":"`+testSessionID+`","goal":"gateway","name":"Synthetic wizard"}`)); err != nil {
		t.Fatal(err)
	}
	system, err := r.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := model.EncodeMember(&system.SystemConfig, "config_token")
	var token string
	if err != nil || json.Unmarshal(raw, &token) != nil || len(token) < 16 {
		t.Fatal("invalid scalar became a credential", err)
	}
}
