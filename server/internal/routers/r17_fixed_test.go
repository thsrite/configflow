package routers

import (
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/routerfake"
)

var _ FixedIPDriver = (*RouterOS)(nil)
var _ FixedIPDriver = (*Ikuai)(nil)

func r17ROSState(dynamic bool) routerfake.State {
	rows := []routerfake.Row{}
	if dynamic {
		rows = append(rows, routerfake.Row{".id": "*10", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "server": "dhcp-example", "dynamic": "true", "disabled": "false", "comment": "", "client-id": "synthetic-client"})
	}
	return routerfake.State{Tables: map[string][]routerfake.Row{rosDHCPServers: {{".id": "*1", "name": "dhcp-example", "interface": "bridge", "disabled": "false"}}, rosDHCPNetworks: {{".id": "*2", "address": "192.0.2.0/24", "gateway": "192.0.2.1"}}, "/ip/address": {{".id": "*3", "address": "192.0.2.1/24", "interface": "bridge"}}, fixedROSLeases: rows, "/ip/arp": {{".id": "*11", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "interface": "bridge"}}}}
}
func TestR17ROSReservationLifecycle(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(map[bool]string{false: "add", true: "convert"}[dynamic], func(t *testing.T) {
			f := routerfake.New(nil)
			f.Reset(r17ROSState(dynamic))
			srv := httptest.NewServer(f)
			defer srv.Close()
			d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			op, err := d.PreviewFixedIP(t.Context(), "192.0.2.21", "02:00:00:00:00:21", "Synthetic")
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := d.ApplyFixedIP(t.Context(), op)
			if err != nil {
				t.Fatal(err)
			}
			deletion, err := d.PreviewDeleteFixedIP(t.Context(), receipt)
			if err != nil {
				t.Fatal(err)
			}
			removed, err := d.DeleteFixedIP(t.Context(), deletion)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := d.RestoreDeletedFixedIP(t.Context(), removed)
			if err != nil || restored.ID == "" {
				t.Fatal(err, restored)
			}
			if err = d.RollbackFixedIP(t.Context(), &FixedIPBinding{Vendor: restored.Vendor, ID: restored.ID, IP: restored.IP, MAC: restored.MAC, Comment: restored.Comment, Server: restored.Server, Undo: receipt.Undo}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func r17IKState()routerfake.IkuaiState{return routerfake.IkuaiState{Tables:map[string][]routerfake.Row{"dhcp_server":{{"id":1,"enabled":"yes","status":1,"interface":"lan1","check_relay_only":0,"addr_pool":"192.0.2.10-192.0.2.200","netmask":"255.255.255.0","gateway":"192.0.2.1"}},"dhcp_static":{},"dhcp_lease":{{"ip_addr":"192.0.2.21","mac":"02:00:00:00:00:21","interface":"lan1"}},"monitor_lanip":{}}}}
func TestR17IkuaiReservationLifecycle(t *testing.T){f:=routerfake.NewIkuai(r17IKState());srv:=httptest.NewServer(f);defer srv.Close();d:=NewIkuai(srv.URL,routerfake.IkuaiUser,routerfake.IkuaiPassword,false);defer d.Close();op,err:=d.PreviewFixedIP(t.Context(),"192.0.2.21","02:00:00:00:00:21","Synthetic");if err!=nil{t.Fatal(err)};b,err:=d.ApplyFixedIP(t.Context(),op);if err!=nil{t.Fatal(err)};del,err:=d.PreviewDeleteFixedIP(t.Context(),b);if err!=nil{t.Fatal(err)};removed,err:=d.DeleteFixedIP(t.Context(),del);if err!=nil{t.Fatal(err)};restored,err:=d.RestoreDeletedFixedIP(t.Context(),removed);if err!=nil||restored.ID==""{t.Fatal(err,restored)};restored.Undo=b.Undo;if err=d.RollbackFixedIP(t.Context(),restored);err!=nil{t.Fatal(err)}}
