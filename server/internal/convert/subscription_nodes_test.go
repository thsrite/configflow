package convert

import (
	"encoding/base64"
	"fmt"
	"testing"

	"configflow/internal/model"
)

func TestN89ParsedVMessWrongServerKeepsOwnerAndValidation(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{Clients: defaultClientCollaborators()}))
	text := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":[],"port":443,"id":"synthetic-uuid"}`))
	node := model.Node{ID: "owner", Name: "Outer", ProxyString: &text}
	line, section, err := SurgeNode(node)
	want := fmt.Sprintf(msgMihomoStoredText, kindNode, "owner", "server", "[]")
	if line != "" || section != "" || err == nil || err.Error() != want {
		t.Fatalf("business validation lost: %q %q %v", line, section, err)
	}
	line, err = LoonNode(node)
	if line != "" || err == nil || err.Error() != want {
		t.Fatalf("Loon validation lost: %q %v", line, err)
	}
}
