// Package agentfake supplies a loopback Agent protocol receiver for boundary
// tests. It validates uploads without activating a real service.
package agentfake

import (
 "archive/tar"
 "bytes"
 "compress/gzip"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "path"
 "regexp"
 "strings"
 "sync"
 "time"
)

// File is an Agent manifest entry.
type File struct {Path string `json:"path"`;Size int64 `json:"size"`;SHA256 string `json:"sha256"`;Role string `json:"role"`}
// Manifest is the Agent's archive target contract.
type Manifest struct {
 ProtocolVersion int `json:"protocol_version"`
 DeploymentID string `json:"deployment_id"`
 AgentID string `json:"agent_id"`
 ProfileID json.RawMessage `json:"profile_id"`
 ServiceType string `json:"service_type"`
 ConfigPath string `json:"config_path"`
 FinalConfig bool `json:"final_config,omitempty"`
 ReloadOnly bool `json:"reload_only,omitempty"`
 Files []File `json:"files"`
}
// Call records an observed request and its validated upload content.
type Call struct {Method,Path string;Headers http.Header;BodySHA256 string;Manifest *Manifest;FileDigests map[string]string;At time.Time}
// Options fixes this fake's identity and advertised capability response.
type Options struct {AgentID,Token,ServiceType,ConfigPath string;Capabilities json.RawMessage}
// Agent is a stateful isolated HTTP handler; mu protects the transcript and journal.
type Agent struct {options Options;mu sync.Mutex;calls []Call;journal map[string]state}
type state struct {Digest,Status string}
var idPattern=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$`)
var digestPattern=regexp.MustCompile(`^[0-9a-f]{64}$`)

// New constructs a fake without starting a listener.
func New(o Options)*Agent {if o.ServiceType==""{o.ServiceType="mihomo"};if o.ConfigPath==""{o.ConfigPath="config.yaml"};return &Agent{options:o,journal:map[string]state{}}}
// Calls returns a JSON-deep-copy of the transcript, safe for concurrent readers.
func(a *Agent)Calls()[]Call {a.mu.Lock();defer a.mu.Unlock();data,_:=json.Marshal(a.calls);var out []Call;_ =json.Unmarshal(data,&out);return out}
func sha(data []byte)string {sum:=sha256.Sum256(data);return hex.EncodeToString(sum[:])}
func safe(p string)bool {lower:=strings.ToLower(p);return p!=""&&p!="."&&p==path.Clean(p)&&!strings.HasPrefix(p,"/")&&!strings.Contains(p,`\`)&&!strings.ContainsRune(p,0)&&p!=".."&&!strings.HasPrefix(p,"../")&&!strings.HasPrefix(lower,".configflow-")}
func respond(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ =json.NewEncoder(w).Encode(v)}

// ServeHTTP implements capabilities and the archive upload receiver. Later A14
// steps extend actions and scripted faults; each instance has its own journal.
func(a *Agent)ServeHTTP(w http.ResponseWriter,r *http.Request) {
 body,err:=io.ReadAll(io.LimitReader(r.Body,(512<<20)+1));if err!=nil||len(body)>512<<20{respond(w,400,map[string]any{"success":false,"message":"invalid upload"});return}
 call:=Call{Method:r.Method,Path:r.URL.Path,Headers:r.Header.Clone(),BodySHA256:sha(body),At:time.Now()}
 defer func(){a.mu.Lock();a.calls=append(a.calls,call);a.mu.Unlock()}()
 if a.options.Token!=""&&r.Header.Get("Authorization")!="Bearer "+a.options.Token{respond(w,401,map[string]any{"success":false});return}
 if r.Method==http.MethodGet&&r.URL.Path=="/api/capabilities" {
  if a.options.Capabilities!=nil{respond(w,200,a.options.Capabilities);return}
  respond(w,200,map[string]any{"deployment_protocols":[]int{1},"final_config_protocols":[]int{1},"reload_only_protocols":[]int{1},"service_type":a.options.ServiceType,"config_path":a.options.ConfigPath});return
 }
 if r.Method!=http.MethodPost||r.URL.Path!="/api/deployments"{respond(w,404,map[string]any{"success":false});return}
 id:=r.Header.Get("X-Deployment-ID");sum:=sha(body)
 if !idPattern.MatchString(id)||strings.ToLower(r.Header.Get("X-Content-SHA256"))!=sum {respond(w,400,map[string]any{"success":false,"message":"invalid deployment header or digest"});return}
 a.mu.Lock();defer a.mu.Unlock()
 if prior,ok:=a.journal[id];ok {if prior.Digest!=sum{respond(w,409,map[string]any{"success":false,"message":"deployment ID already belongs to different content"});return};respond(w,200,map[string]any{"success":true,"deployment_id":id,"status":prior.Status});return}
 m,files,err:=extract(body)
 if err==nil&&(m.ProtocolVersion!=1||m.DeploymentID!=id||m.ServiceType!=a.options.ServiceType||m.ConfigPath!=a.options.ConfigPath||(a.options.AgentID!=""&&m.AgentID!=a.options.AgentID)){err=fmt.Errorf("deployment target or protocol does not match Agent")}
 if err!=nil{respond(w,400,map[string]any{"success":false,"message":err.Error()});return}
 call.Manifest=&m;call.FileDigests=files
 a.journal[id]=state{sum,"verifying"}
 respond(w,202,map[string]any{"success":true,"deployment_id":id,"status":"verifying"})
}

// extract follows go-agent/routes.extractDeployment: regular files only, safe
// relative paths, expanded/header limits, strict flags, target checks by caller,
// file count, lowercase digests, exact byte sizes and a listed config file.
func extract(body []byte)(Manifest,map[string]string,error) {
 var m Manifest;fail:=func(s string)(Manifest,map[string]string,error){return m,nil,fmt.Errorf("%s",s)}
 gz,err:=gzip.NewReader(bytes.NewReader(body));if err!=nil{return fail("invalid gzip archive")};defer gz.Close();tr:=tar.NewReader(gz)
 var meta []byte;seen:=map[string]bool{};actual:=map[string]File{};var expanded int64;headers:=0
 for {h,err:=tr.Next();if err==io.EOF{break};if err!=nil{return fail("invalid tar archive")};headers++;if headers>20000{return fail("too many archive entries")}
  if h.Typeflag==tar.TypeDir {n:=strings.TrimSuffix(h.Name,"/");if n!="files"&&(!strings.HasPrefix(n,"files/")||!safe(strings.TrimPrefix(n,"files/"))){return fail("invalid archive directory")};continue}
  if h.Typeflag!=tar.TypeReg&&h.Typeflag!=tar.TypeRegA{return fail("archive links and special files are forbidden")};if seen[h.Name]{return fail("duplicate archive path")};seen[h.Name]=true
  expanded+=h.Size;if h.Size<0||expanded>1<<30{return fail("expanded deployment exceeds size limit")}
  if h.Name=="manifest.json"{if h.Size>4<<20{return fail("manifest exceeds size limit")};meta,err=io.ReadAll(tr);if err!=nil{return fail("invalid manifest")};continue}
  if !strings.HasPrefix(h.Name,"files/")||!safe(strings.TrimPrefix(h.Name,"files/")){return fail("invalid archive file path")}
  hash:=sha256.New();n,err:=io.Copy(hash,tr);if err!=nil{return fail("incomplete deployment file")};p:=strings.TrimPrefix(h.Name,"files/");actual[p]=File{Path:p,Size:n,SHA256:hex.EncodeToString(hash.Sum(nil))}
 }
 if n,err:=io.Copy(io.Discard,io.LimitReader(gz,(1<<20)+1));err!=nil||n>1<<20{return fail("invalid gzip trailer")}
 var flags map[string]json.RawMessage;if json.Unmarshal(meta,&flags)!=nil{return fail("invalid manifest")}
 for _,key:=range []string{"reload_only","final_config"}{if flag,ok:=flags[key];ok&&string(flag)!="true"&&string(flag)!="false"{return fail(key+" must be a boolean")}}
 if json.Unmarshal(meta,&m)!=nil{return fail("invalid manifest")};if len(m.Files)==0||len(m.Files)>10000||len(m.Files)!=len(actual){return fail("manifest file count mismatch")}
 listed:=map[string]bool{};hasConfig:=false;digests:=map[string]string{}
 for _,entry:=range m.Files {if !safe(entry.Path)||listed[entry.Path]||entry.Size<0||!digestPattern.MatchString(entry.SHA256){return fail("invalid manifest file")};listed[entry.Path]=true;f,ok:=actual[entry.Path];if !ok||f.Size!=entry.Size||f.SHA256!=entry.SHA256{return fail("manifest checksum mismatch")};hasConfig=hasConfig||entry.Path==m.ConfigPath;digests[entry.Path]=f.SHA256}
 if !hasConfig{return fail("configuration file missing from manifest")};return m,digests,nil
}
