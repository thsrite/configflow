package devhttp

import "testing"

func TestAuditDeviceReplyExactMembers(t *testing.T) {
	var reply struct {
		ID    string `json:"id"`
		Proof struct {
			OK bool `json:"ok"`
		} `json:"proof"`
	}
	r := Response{Body: []byte(`{"id":"proven","ID":"spoofed","proof":{"OK":true}}`)}
	if err := r.JSON(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.ID != "proven" || reply.Proof.OK {
		t.Fatalf("case variant authorized a reply: %#v", reply)
	}
}
