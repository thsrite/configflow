package installer

import (
	"gopkg.in/yaml.v3"
	"iter"
	"strings"

	"configflow/internal/yamlx"
)

type composeDocument struct {
	Version  string                        `yaml:"version,omitempty"`
	Name     string                        `yaml:"name,omitempty"`
	Services composeServices               `yaml:"services"`
	Networks map[string]*composeNetwork    `yaml:"networks,omitempty"`
	Volumes  map[string]composeNamedVolume `yaml:"volumes,omitempty"`
}

type composeServices struct {
	Agent     *composeService `yaml:"agent,omitempty"`
	Companion *composeService `yaml:"ha-companion,omitempty"`
	Init      *composeService `yaml:"install-init,omitempty"`
}

func (s composeServices) all() iter.Seq2[string, *composeService] {
	return func(yield func(string, *composeService) bool) {
		for _, item := range []struct {
			name    string
			service *composeService
		}{{"agent", s.Agent}, {"ha-companion", s.Companion}, {"install-init", s.Init}} {
			if item.service != nil && !yield(item.name, item.service) {
				return
			}
		}
	}
}

type composeService struct {
	Image              *yamlx.Node               `yaml:"image"`
	ContainerName      string                    `yaml:"container_name,omitempty"`
	Restart            string                    `yaml:"restart"`
	NetworkMode        string                    `yaml:"network_mode"`
	Environment        composeEnvironment        `yaml:"environment"`
	Volumes            []composeMount            `yaml:"volumes"`
	Logging            *composeLogging           `yaml:"logging,omitempty"`
	CapAdd             []string                  `yaml:"cap_add,omitempty"`
	StopGracePeriod    string                    `yaml:"stop_grace_period,omitempty"`
	Networks           map[string]composeAddress `yaml:"networks,omitempty"`
	DependsOn          composeDependencies       `yaml:"depends_on,omitempty"`
	Ports              []string                  `yaml:"ports,omitempty"`
	Labels             map[string]string         `yaml:"labels,omitempty"`
	Entrypoint         []string                  `yaml:"entrypoint,omitempty"`
	Command            *[]string                 `yaml:"command,omitempty"`
	EnvFile            []composeEnvFile          `yaml:"env_file,omitempty"`
	ReadOnly           bool                      `yaml:"read_only,omitempty"`
	User               string                    `yaml:"user,omitempty"`
	CapDrop            []string                  `yaml:"cap_drop,omitempty"`
	SecurityOpt        []string                  `yaml:"security_opt,omitempty"`
	owner, networkName string
}

type composeMount struct {
	Short    string                `yaml:"-"`
	Type     string                `yaml:"type"`
	Source   string                `yaml:"source"`
	Target   string                `yaml:"target"`
	ReadOnly bool                  `yaml:"read_only"`
	Bind     *composeBind          `yaml:"bind,omitempty"`
	Volume   *composeVolumeOptions `yaml:"volume,omitempty"`
	flowBind bool
}
type composeBind struct {
	CreateHostPath bool `yaml:"create_host_path"`
}
type composeVolumeOptions struct {
	NoCopy bool `yaml:"nocopy"`
}

func (m composeMount) MarshalYAML() (any, error) {
	if m.Short != "" {
		return yamlx.Str(m.Short), nil
	}
	type plain composeMount
	var node yamlx.Node
	if err := node.Encode(plain(m)); err != nil {
		return nil, err
	}
	if m.flowBind {
		if bind := yamlx.Get(&node, "bind"); bind != nil {
			bind.Style |= yaml.FlowStyle
		}
	}
	return &node, nil
}
func bindMount(source, target string, readonly, create bool) composeMount {
	return composeMount{Type: "bind", Source: source, Target: target, ReadOnly: readonly, Bind: &composeBind{create}}
}

type composeEnvironment struct {
	Entries []string
	Mapping bool
}

func (e composeEnvironment) MarshalYAML() (any, error) {
	if !e.Mapping {
		return e.Entries, nil
	}
	node := yamlx.Map()
	for _, entry := range e.Entries {
		key, value, _ := strings.Cut(entry, "=")
		yamlx.Set(node, key, yamlx.Str(value))
	}
	return node, nil
}
func (e *composeEnvironment) set(key, value string) {
	for i, entry := range e.Entries {
		name, _, _ := strings.Cut(entry, "=")
		if name == key {
			e.Entries[i] = key + "=" + value
			return
		}
	}
	e.Entries = append(e.Entries, key+"="+value)
}

type composeDependencies struct {
	Names      []string
	Conditions []composeDependency
}
type composeDependency struct{ Name, Condition string }

func (d composeDependencies) IsZero() bool { return len(d.Names) == 0 && len(d.Conditions) == 0 }
func (d composeDependencies) MarshalYAML() (any, error) {
	if len(d.Conditions) == 0 {
		return d.Names, nil
	}
	node := yamlx.Map()
	for _, dep := range d.Conditions {
		yamlx.Set(node, dep.Name, yamlx.Map(yamlx.Str("condition"), yamlx.Str(dep.Condition)))
	}
	return node, nil
}

type composeEnvFile struct {
	Path   string `yaml:"path"`
	Format string `yaml:"format"`
}
type composeAddress struct {
	IPv4Address *yamlx.Node `yaml:"ipv4_address"`
}
type composeLogging struct {
	Driver  string            `yaml:"driver"`
	Options composeLogOptions `yaml:"options"`
}
type composeLogOptions struct {
	MaxSize string `yaml:"max-size"`
	MaxFile string `yaml:"max-file"`
}
type composeNetwork struct {
	Name       string            `yaml:"name"`
	Driver     string            `yaml:"driver"`
	DriverOpts composeDriverOpts `yaml:"driver_opts"`
	IPAM       composeIPAM       `yaml:"ipam"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}
type composeDriverOpts struct {
	Parent string `yaml:"parent"`
}
type composeIPAM struct {
	Config []composeSubnet `yaml:"config"`
}
type composeSubnet struct {
	Subnet  string `yaml:"subnet"`
	Gateway string `yaml:"gateway,omitempty"`
}
type composeNamedVolume struct {
	External bool   `yaml:"external,omitempty"`
	Name     string `yaml:"name"`
	Driver   string `yaml:"driver,omitempty"`
}

func composeYAML(document *composeDocument, escape bool) (string, error) {
	var node yamlx.Node
	if err := node.Encode(document); err != nil {
		return "", err
	}
	raw, err := yamlx.Marshal(&node)
	text := string(raw)
	if escape {
		text = strings.ReplaceAll(text, "$", "$$")
	}
	return text, err
}

func yamlString(text string) *yamlx.Node { return yamlx.Str(text) }
func nodeText(node *yamlx.Node) string {
	node = yamlx.Resolve(node)
	if node == nil {
		return ""
	}
	return node.Value
}

func (s composeService) MarshalYAML() (any, error) {
	type plain composeService
	var node yamlx.Node
	if err := node.Encode(plain(s)); err != nil {
		return nil, err
	}
	if len(s.Networks) > 0 {
		yamlx.Delete(&node, "network_mode")
	}
	return &node, nil
}
