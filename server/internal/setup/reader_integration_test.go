package setup

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/lockx"
	"configflow/internal/setupflow"
)

func TestAtomicWizardRecordsUseSharedReadOnlyReader(t *testing.T) {
	s, ctx := serviceFixture(t, "gateway")
	store := storeFor(s.repository)
	before := lockx.Violations()
	if err := store.locked(ctx, testSessionID, func(held context.Context) error {
		// The shared reader must never acquire a second session operation lock.
		sessions, err := setupflow.ProfileSessions(held, s.repository.DataDir(), "setup_"+testSessionID[:20])
		if err != nil {
			return err
		}
		if len(sessions) != 1 {
			t.Fatal("C36 record missing from A20 reader")
		}
		var record Record
		if err := json.Unmarshal(sessions[0].Raw, &record); err != nil {
			return err
		}
		record.LocalDNS = new("192.0.2.53")
		return store.write(held, &record)
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := setupflow.ProfileSessions(ctx, s.repository.DataDir(), "setup_"+testSessionID[:20])
	if err != nil || len(sessions) != 1 {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(sessions[0].Raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["local_dns"]) != `"192.0.2.53"` {
		t.Fatal("stored member written under the lock lost")
	}
	if lockx.Violations() != before {
		t.Fatal("shared read/write integration violated lock order")
	}
}
