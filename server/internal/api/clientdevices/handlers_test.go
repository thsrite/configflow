package clientdevices_test

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 api "configflow/internal/api/clientdevices"
 "configflow/internal/httpx/httpxtest"
 "configflow/internal/jsonx"
 "configflow/internal/routers"
 "configflow/internal/routers/routerfake"
)
func rosState()routerfake.State{return routerfake.State{Tables:map[string][]routerfake.Row{"/ip/dhcp-server":{{".id":"*1","name":"dhcp-example","interface":"bridge","disabled":"false"}},"/ip/dhcp-server/network":{{".id":"*2","address":"192.0.2.0/24","gateway":"192.0.2.1"}},"/ip/address":{{".id":"*3","address":"192.0.2.1/24","interface":"bridge"}},"/ip/dhcp-server/lease":{{".id":"*10","address":"192.0.2.21","mac-address":"02:00:00:00:00:21","server":"dhcp-example","dynamic":"true","disabled":"false","comment":""}},"/ip/arp":{{".id":"*11","address":"192.0.2.21","mac-address":"02:00:00:00:00:21","interface":"bridge","status":"reachable"}}}}}
func fixture(t *testing.T)(*httpxtest.Server,*routerfake.Fake){t.Helper();f:=routerfake.New(nil);f.Reset(rosState());device:=httptest.NewServer(f);t.Cleanup(device.Close);s:=httpxtest.New(t,httpxtest.Options{Auth:true,SecretKey:"synthetic-fixed-secret",Register:httpxtest.Routes(api.Register)});routers.Wire();_,_,err:=s.Repo.UpdateProfileTransaction(s.Context(),"default",func(_ context.Context,current *jsonx.Object)(*jsonx.Object,error){current.Set("device_notes",jsonx.Obj("192.0.2.21","Synthetic"));current.Set("routers",jsonx.Obj("items",[]any{jsonx.Obj("id","router-one","name","Synthetic","type","routeros","endpoint",device.URL,"username","synthetic-user","password","synthetic-pass","verify_tls",false)}));return nil,nil});if err!=nil{t.Fatal(err)};return s,f}
func requestBody(operation string)any{return map[string]any{"profile_id":"default","operation":operation,"devices":[]any{map[string]any{"ip":"192.0.2.21","mac":"02:00:00:00:00:21","router_id":"router-one","name":"Synthetic"}}}}
func TestR17HTTPRoutes(t *testing.T){s,f:=fixture(t);login:=s.Login();s.Get("/api/client-devices").WantStatus(401);s.Get("/api/client-devices",login).WantStatus(200);before,err:=s.Repo.ConfigRevision(s.Context(),"default");if err!=nil{t.Fatal(err)};head:=s.Do("HEAD","/api/client-devices",login,httpxtest.JSON(map[string]any{"profile_id":"default","ip":"192.0.2.21","name":"must-not-save","mode":"","previous_name":"Synthetic","previous_mode":"","base_url":""}));head.WantStatus(200);if len(head.Body)!=0{t.Fatal("HEAD returned body")};after,_:=s.Repo.ConfigRevision(s.Context(),"default");if before!=after{t.Fatal("HEAD wrote profile")};if head.Header.Get("Cache-Control")!="no-store"{t.Fatal("missing no-store")}
 preview:=s.Post("/api/client-devices/fixed-ip/preview",login,httpxtest.JSON(requestBody("add")));preview.WantStatus(200);token:=preview.Object().GetString("token");if token==""||token=="[REDACTED]"{t.Fatal("preview token missing/redacted")};s.Post("/api/client-devices/fixed-ip/apply",login,httpxtest.JSON(map[string]string{"profile_id":"default","token":token})).WantStatus(200);stored,err:=s.Repo.GetProfile(s.Context(),"default");if err!=nil{t.Fatal(err)};binding:=stored.GetObject("client_fixed_ips").GetObject("192.0.2.21");if binding==nil||binding.GetString("router_id")!="router-one"{t.Fatalf("binding %+v",binding)};s.Post("/api/client-devices/fixed-ip/apply",login,httpxtest.JSON(map[string]string{"profile_id":"default","token":token})).WantStatus(502)
 discovery:=s.Get("/api/client-devices/discovery?refresh=1",login);discovery.WantStatus(200);if !strings.Contains(discovery.Text(),`"owned":true`){t.Fatalf("fixed enrichment %s",discovery.Body)};deletion:=s.Post("/api/client-devices/fixed-ip/preview",login,httpxtest.JSON(requestBody("delete")));deletion.WantStatus(200);s.Post("/api/client-devices/fixed-ip/apply",login,httpxtest.JSON(map[string]string{"profile_id":"default","token":deletion.Object().GetString("token")})).WantStatus(200);if len(f.Snapshot().Tables["/ip/dhcp-server/lease"])!=0{t.Fatal("owned reservation remains")}
 for _,call:=range f.Calls(){if call.Method==http.MethodGet||call.Method==http.MethodDelete{if call.Body!=nil{t.Fatal("GET/DELETE body")}}}
}
func TestR17HTTPFailureShapesAndPeerSecrets(t *testing.T){s,_:=fixture(t);login:=s.Login();for _,tc:=range []struct{path,body string;status int}{{"/api/client-devices",`[]`,400},{"/api/client-devices/fixed-ip/preview",`{}`,400},{"/api/client-devices/fixed-ip/apply",`{"profile_id":"other","token":"bad"}`,409},{"/api/client-devices/fixed-ip/apply",`{"profile_id":"default","token":"bad"}`,409}}{method:="POST";if tc.path=="/api/client-devices"{method="PUT"};s.Do(method,tc.path,login,httpxtest.Body([]byte(tc.body),"application/json")).WantStatus(tc.status)}
 _,_,err:=s.Repo.UpdateProfileTransaction(s.Context(),"default",func(_ context.Context,current *jsonx.Object)(*jsonx.Object,error){current.Set("device_notes",jsonx.Obj("192.0.2.21","reference synthetic-pass"));return nil,nil});if err!=nil{t.Fatal(err)};result:=s.Get("/api/client-devices",login);result.WantStatus(200);if strings.Contains(result.Text(),"synthetic-pass"){t.Fatal("public list leaked a configured credential")};var doc map[string]json.RawMessage;if json.Unmarshal(result.Body,&doc)!=nil{t.Fatal("invalid public response")}
}
