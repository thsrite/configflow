package routers

import (
	"bytes"
	"configflow/internal/routers/rospolicy"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/routerfake"
)

func TestO36bNATAndEmptyOwnerRuntimeBoundary(t *testing.T) {
	for _, operation := range []string{"addresses-create", "addresses-delete", "nat-delete", "nat-apply", "nat-restore", "empty-restore", "empty-apply-many"} {
		for _, shutdown := range []bool{true, false} {
			name := operation + "/disconnect"
			if shutdown {
				name = operation + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				fake := routerfake.New(r07FakeState().Tables)
				server := httptest.NewServer(fake)
				defer server.Close()
				d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
				defer d.Close()
				runtime, request, disconnect := o36bRuntime(t)
				before, _ := json.Marshal(fake.Snapshot().Tables)
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				var journal []NATStep
				var err error
				switch operation {
				case "addresses-create":
					_, err = d.CreateAddresses(request, nil, &journal, ROSIDs{})
				case "addresses-delete":
					err = d.DeleteAddresses(request, nil, &journal)
				case "nat-delete":
					err = rospolicy.DeleteNATRules(request, d.Client, nil, &journal)
				case "nat-apply":
					var undo *NATListUndo
					undo, err = rospolicy.ApplyNATLists(request, d.Client, r07NewSync())
					if err == nil {
						err = rospolicy.RestoreNATLists(request, d.Client, *undo)
					}
				case "nat-restore":
					err = rospolicy.RestoreNATLists(request, d.Client, NATListUndo{Kind: "nat_list_restore"})
				case "empty-restore":
					_, err = d.Apply(request, json.RawMessage(`{"kind":"ros_restore","changes":[]}`))
				case "empty-apply-many":
					err = d.ApplyMany(request, nil, nil)
				}
				after, _ := json.Marshal(fake.Snapshot().Tables)
				if shutdown {
					if !errors.Is(err, context.Canceled) || len(fake.Calls()) != 0 || !bytes.Equal(before, after) || d.NATListTouched() || len(journal) != 0 {
						t.Fatalf("stopped owner changed device/journal/evidence: err=%v touched=%t", err, d.NATListTouched())
					}
				} else if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("disconnected write/restore failed: %v", err)
				}
			})
		}
	}
}
