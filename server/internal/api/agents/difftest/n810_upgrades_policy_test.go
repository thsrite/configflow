package difftest

import (
 "crypto/sha256"
 "fmt"
 "os"
 "strings"
 "testing"
 "configflow/internal/jsonx"
)
var upgradeNativeNames=map[string]bool{"update":true,"report":true,"upgrade poll rolling back":true,"review upgrade string record":true,"review upgrade pairs record":true,"review upgrade int record":true,"review update info without architecture":true,"review update again after stuck reservation":true}
func upgradePolicyInput(r upgradesRequest)*jsonx.Object{headers:=jsonx.NewObject();for k,v:=range r.headers{headers.Set(k,v)};return jsonx.Obj("name",r.name,"method",r.method,"path",r.path,"body",r.body,"content_type",r.contentType,"admin",r.admin,"headers",headers,"mask",jsonx.ListOf(r.mask),"phase",r.phase,"wait",int64(r.wait))}
func captureUpgradePython(t *testing.T,r upgradesRequest,py response,calls []string,dir string){
 t.Helper();path:=os.Getenv("N810_RECORD_UPGRADES_PYTHON");if path=="" || !upgradeNativeNames[r.name]{return}
 var body any=string(py.body)
 if parsed,err:=jsonx.Parse(py.body);err==nil{body=maskJSON(parsed,r.mask)}
 states:=upgradeAgents(t,dir).([]any)
 id:=strings.Split(strings.TrimPrefix(r.path,"/api/agents/"),"/")[0]
 var state any
 for _,item:=range states{a:=item.(*jsonx.Object);if a.GetString("name")==id{state=a.Value("latest_upgrade");break}}
 row:=jsonx.Obj("input",upgradePolicyInput(r),"python_status",py.status,"python_type",py.header.Get("Content-Type"),"python_body",body,"python_calls",jsonx.ListOf(calls),"stored_upgrade",state)
 raw,err:=jsonx.Compact(row);if err!=nil{t.Fatal(err)}
 file,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600);if err!=nil{t.Fatal(err)}
 defer file.Close();if _,err:=file.Write(append(raw,'\n'));err!=nil{t.Fatal(err)}
}
// loadUpgradePolicy binds every complete original Python HTTP row. Expected
// native values are generated independently by the accompanying Python policy.
func loadUpgradePolicy(t *testing.T,r upgradesRequest)*jsonx.Object{
 t.Helper();if !upgradeNativeNames[r.name] || r.name=="update"{return nil}
 if os.Getenv("N810_RECORD_UPGRADES_PYTHON")!=""{return nil}
 raw,err:=os.ReadFile("testdata/n810_upgrades_http_native.json");if err!=nil{t.Fatal(err)}
 parsed,err:=jsonx.Parse(raw);if err!=nil{t.Fatal(err)};doc:=parsed.(*jsonx.Object)
 original,err:=os.ReadFile("testdata/n810_upgrades_http_python.jsonl");if err!=nil || fmt.Sprintf("%x",sha256.Sum256(original))!=doc.GetString("archive_sha256") || doc.GetString("archive_sha256")!="8b8e839a2673df5020697f909de8e2e85d5ae84d17d071b3f81797237e1b6688"{t.Fatal("Python HTTP archive changed",err)}
 for _,entry:=range doc.GetList("rows"){
  row:=entry.(*jsonx.Object);old:=row.GetObject("original")
  encoded,_:=jsonx.Compact(old)
  if fmt.Sprintf("%x",sha256.Sum256(encoded))!=row.GetString("row_sha256"){t.Fatal("Python HTTP row changed")}
  if old.GetObject("input").GetString("name")!=r.name{continue}
  if !jsonx.Equal(old.Value("input"),upgradePolicyInput(r)){return nil}
  return row
 }
 return nil
}
func compareUpgradePolicy(t *testing.T,r upgradesRequest,row *jsonx.Object,py,goResponse response,pyCalls,goCalls []string)[]string{
 t.Helper();original:=row.GetObject("original");native:=row.GetObject("native");var differences []string
 var pyBody any=string(py.body);if parsed,err:=jsonx.Parse(py.body);err==nil{pyBody=maskJSON(parsed,r.mask)}
 if py.status!=int(original.GetInt("python_status")) || py.header.Get("Content-Type")!=original.GetString("python_type") || !jsonx.Equal(pyBody,original.Value("python_body")) || !jsonx.Equal(jsonx.ListOf(pyCalls),original.Value("python_calls")){differences=append(differences,"original Python HTTP boundary changed")}
 goBody,err:=jsonx.Parse(goResponse.body)
 if err!=nil || goResponse.status!=int(native.GetInt("status")) || goResponse.header.Get("Content-Type")!=native.GetString("type") || !jsonx.Equal(maskJSON(goBody,r.mask),native.Value("body")){differences=append(differences,fmt.Sprintf("native HTTP boundary differs: %d %s",goResponse.status,goResponse.body))}
 if !jsonx.Equal(jsonx.ListOf(goCalls),native.Value("calls")){differences=append(differences,fmt.Sprintf("native request trace differs: %q",goCalls))}
 for _,header:=range []string{"Cache-Control","X-ConfigFlow-Profile"}{if py.header.Get(header)!=goResponse.header.Get(header){differences=append(differences,"unrelated response header changed")}}
 return differences
}
func adjustUpgradeStoredPolicy(t *testing.T,python,native any){
 t.Helper();if os.Getenv("N810_RECORD_UPGRADES_PYTHON")!=""{return}
 raw,err:=os.ReadFile("testdata/n810_upgrades_http_native.json");if err!=nil{t.Fatal(err)}
 parsed,err:=jsonx.Parse(raw);if err!=nil{t.Fatal(err)};doc:=parsed.(*jsonx.Object)
 for _,entry:=range doc.GetList("stored"){
  rule:=entry.(*jsonx.Object)
  var pa,ga *jsonx.Object
  for _,item:=range python.([]any){a:=item.(*jsonx.Object);if a.GetString("name")==rule.GetString("agent"){pa=a}}
  for _,item:=range native.([]any){a:=item.(*jsonx.Object);if a.GetString("name")==rule.GetString("agent"){ga=a}}
  if pa==nil || ga==nil || !jsonx.Equal(pa.Value("latest_upgrade"),rule.Value("python")) || !jsonx.Equal(ga.Value("latest_upgrade"),rule.Value("native")){t.Fatal("approved stored upgrade delta changed",rule.GetString("agent"))}
  // Only after checking both complete subtrees, align that one known subtree
  // for the surrounding all-Agent comparison. No input or oracle is rewritten.
  pa.Set("latest_upgrade",ga.Value("latest_upgrade"))
 }
}
func TestN810UpgradesHTTPPolicyNegativeControl(t *testing.T){
 seen:=0
 for _,r:=range upgradesRequests("192.0.2.55"){
  if row:=loadUpgradePolicy(t,r);row!=nil{seen++;forged:=r;forged.body+="forged";if loadUpgradePolicy(t,forged)!=nil{t.Fatal("forged request still selected native policy")}}
 }
 if seen!=7{t.Fatal("native HTTP rows were not all exercised",seen)}
}
