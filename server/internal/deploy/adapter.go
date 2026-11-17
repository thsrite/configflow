package deploy

// All legacy document boundaries live here; the rest of deploy only sees typed
// values, yamlx nodes, opaque records or raw JSON.
import (
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"encoding/json"
)

// Source is an immutable profile snapshot until the typed repository lands.
type Source struct {
	legacy   *jsonx.Object
	revision *int64
}

// NewSource captures a legacy profile and its optional revision.
func NewSource(config *jsonx.Object, revision *int64) *Source {
	s := &Source{}
	if config != nil {
		s.legacy = config.DeepCopy()
	}
	if revision != nil {
		r := *revision
		s.revision = &r
	}
	return s
}

// ProfileID returns the snapshot's resolved profile identifier.
func (s *Source) ProfileID() string {
	if s == nil {
		return ""
	}
	return s.legacy.GetString("profile_id")
}
func (s *Source) gateway() *gateway.Config {
	if s == nil {
		return gateway.NewConfig(nil)
	}
	return gateway.NewConfig(s.legacy)
}

// NewTarget captures the Agent fields needed by the publishing chain.
func NewTarget(record *jsonx.Object) Target {
	if record == nil {
		return Target{}
	}
	r := record.DeepCopy()
	port, _ := json.Marshal(r.Value("port"))
	return Target{ID: r.GetString("id"), Name: r.GetString("name"), ProfileID: r.GetString("profile_id"), ServiceType: r.GetString("service_type"), DeploymentMethod: r.GetString("deployment_method"), Version: r.GetString("version"), Host: r.GetString("host"), Port: port, Token: r.GetString("token"), DomainDiscovery: r.GetBool("domain_discovery_enabled"), Record: r}
}
