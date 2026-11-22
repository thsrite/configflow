package installer

import (
	"encoding/json"
	"path"
	"strings"
)

const (
	// ComposeRuntime is the Agent subcommand (Agent 1.9.3-go) that prepares
	// the data root (init container) and verifies and enrolls the Agent
	// container. Before 1.9.3-go it was python3 docker-compose-install.py.
	ComposeRuntime = "ha-compose"
	// ComposeIdentityVolume is shared by installations on the same Docker host.
	ComposeIdentityVolume = "configflow-ha-host-identity"
)

type composeInstallSettings struct {
	Protocol     int     `json:"protocol"`
	Owner        string  `json:"owner"`
	URL          string  `json:"url"`
	IdentityMode string  `json:"identity_mode"`
	Token        *string `json:"token,omitempty"`
}

func generateDirectNative(params GuideOptions, request GuideRequest, enrollment *Capability) (string, *ComposeDeployment, error) {
	data := params.DataDir
	if composeDataDirRefused(data) {
		return "", nil, valueError(composeDataDirMessage)
	}
	identityMode := "file"
	if request.CreateHostIdentity {
		identityMode = "docker-volume"
	}
	if identityMode == "file" {
		if raw, present := params.identity.Get("ha_host_id_path"); present && len(raw) > 0 && raw[0] != '"' {
			return "", nil, typeError(msgRendererText)
		}
	}
	identityCheck, dataCheck := path.Clean(params.HAHostIDPath), path.Clean(data)
	if identityMode == "file" && (identityCheck == dataCheck || strings.HasPrefix(identityCheck, dataCheck+"/")) {
		return "", nil, valueError(msgDirectIdentityLocation)
	}
	normalized := params
	if identityMode != "file" {
		if err := setSessionField(&normalized.identity, "ha_host_id_path", ""); err != nil {
			return "", nil, err
		}
	}
	owner, err := planHash(normalized, map[string]string{"image": request.Image, "enrollment_url": enrollmentURL(enrollment), "install_method": "compose_direct", "identity_mode": identityMode}, true)
	if err != nil {
		return "", nil, err
	}
	project := "cf-oxidns-" + owner[:16]
	base := params
	if identityMode != "file" {
		base.HAHostIDPath = "/etc/machine-id"
	}
	compose, err := guideComposeNative(base)
	if err != nil {
		return "", nil, err
	}
	compose.Version = ""
	compose.Name = project
	settings := composeInstallSettings{Protocol: 1, Owner: owner, URL: enrollmentURL(enrollment), IdentityMode: identityMode}
	identityMount := func(readonly bool) composeMount {
		if identityMode == "file" {
			return bindMount(params.HAHostIDPath, "/run/configflow-host-machine-id", true, false)
		}
		return composeMount{Type: "volume", Source: "host-identity", Target: "/run/configflow-host-identity", ReadOnly: readonly, Volume: &composeVolumeOptions{true}}
	}
	host := params.NetworkMode == "host"
	empty := []string{}
	for name, service := range compose.Services.all() {
		service.Image = imageNode(request)
		service.Labels = map[string]string{ComposeLabel: owner}
		helper := name == "ha-companion"
		service.Volumes = []composeMount{bindMount(data+"/agent", "/opt/configflow-agent", helper, true), bindMount(data+"/oxidns", "/etc/oxidns", helper, true), bindMount(data+"/ha", "/var/lib/configflow-ha", false, true), identityMount(true)}
		env := &service.Environment
		env.Mapping = true
		if identityMode == "docker-volume" {
			env.set("CONFIGFLOW_DNS_HA_IDENTITY", "volume")
		}
		service.DependsOn = composeDependencies{Conditions: []composeDependency{{"install-init", "service_completed_successfully"}}}
		if name == "agent" {
			service.Volumes = append(service.Volumes, bindMount("/proc/sys/kernel/random/boot_id", "/run/configflow-host-boot-id", true, false))
			if host {
				service.Volumes = append(service.Volumes, bindMount("/proc/1/ns/net", "/run/configflow-host-netns", true, false))
			}
			service.Entrypoint = []string{"configflow-agent", ComposeRuntime, "run"}
			service.Command = &empty
			agentSettings := settings
			agentSettings.Token = new(enrollmentToken(enrollment))
			encoded, err := json.Marshal(agentSettings)
			if err != nil {
				return "", nil, err
			}
			env.set("CONFIGFLOW_COMPOSE_INSTALL", string(encoded))
			if host {
				service.DependsOn.Conditions = append(service.DependsOn.Conditions, composeDependency{"ha-companion", "service_started"})
			}
		}
	}
	initSettings, err := json.Marshal(settings)
	if err != nil {
		return "", nil, err
	}
	compose.Services.Init = &composeService{Image: imageNode(request), Entrypoint: []string{"configflow-agent", ComposeRuntime, "prepare"}, Command: &empty,
		Restart: "no", NetworkMode: "none", ReadOnly: true, User: "0:0", CapDrop: []string{"ALL"}, CapAdd: []string{"DAC_OVERRIDE", "FOWNER"}, SecurityOpt: []string{"no-new-privileges:true"},
		Environment: composeEnvironment{Entries: []string{"CONFIGFLOW_COMPOSE_INSTALL=" + string(initSettings)}, Mapping: true},
		Volumes:     []composeMount{bindMount(data, "/configflow-data", false, true), identityMount(false)}, Labels: map[string]string{ComposeLabel: owner}, Logging: &composeLogging{Driver: "json-file", Options: composeLogOptions{"5m", "2"}}}
	if identityMode == "docker-volume" {
		compose.Volumes = map[string]composeNamedVolume{"host-identity": {Name: ComposeIdentityVolume, Driver: "local"}}
	}
	for _, network := range compose.Networks {
		network.Name = project + "-dns-ha"
		network.Labels = map[string]string{ComposeLabel: owner}
	}
	text, err := haDumpCompose(compose)
	if err != nil {
		return "", nil, err
	}
	return text, &ComposeDeployment{Filename: "compose.yaml", ProjectName: project, Command: "docker compose up -d", IdentityMode: identityMode}, nil
}
