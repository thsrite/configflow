package installer

// Port of backend/agents/docker_ha_compose_direct.py (module A10):
// single-file Compose for new installs; all preparation runs inside the image.

import (
	"strings"

	"configflow/internal/jsonx"
)

const (
	// ComposeRuntime is RUNTIME.
	ComposeRuntime = "/usr/local/lib/configflow/docker-compose-install.py"
	// ComposeIdentityVolume is IDENTITY_VOLUME.
	ComposeIdentityVolume = "configflow-ha-host-identity"
)

// GenerateDirect is generate_direct(params, request, enrollment).
func GenerateDirect(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	data := params.GetString("data_dir")
	if composeDataDirRefused(data) {
		return "", nil, valueError(composeDataDirMessage)
	}
	identityMode := "file"
	if jsonx.Truthy(request.GetOr("create_host_identity", nil)) {
		identityMode = "docker-volume"
	}
	if identityMode == "file" {
		identityPath, err := pyConcat(params.Value("ha_host_id_path"))
		if err != nil {
			return "", nil, err
		}
		if identityPath == data || strings.HasPrefix(identityPath, data+"/") {
			return "", nil, valueError("单文件 Compose 的现有宿主身份文件须位于应用数据目录之外")
		}
	}
	image := requestImage(request)
	normalized := params.Clone()
	if identityMode != "file" {
		normalized.Set("ha_host_id_path", "")
	}
	plan := normalized.Clone()
	plan.Set("image", image)
	plan.Set("enrollment_url", enrollmentURL(enrollment))
	plan.Set("install_method", "compose_direct")
	plan.Set("identity_mode", identityMode)
	owner, err := pyJSONHash(plan, true)
	if err != nil {
		return "", nil, err
	}
	project := "cf-oxidns-" + owner[:16]
	base := params.Clone()
	if identityMode != "file" {
		base.Set("ha_host_id_path", "/etc/machine-id")
	}
	compose, err := guideCompose(base)
	if err != nil {
		return "", nil, err
	}
	compose.Delete("version")
	compose.Set("name", project)
	settings := jsonx.Obj("protocol", 1, "owner", owner, "url", enrollmentURL(enrollment), "identity_mode", identityMode)

	bind := func(source, target string, readonly, create bool) *jsonx.Object {
		result := guideVolume(jsonx.Obj("type", "bind", "source", source), target, readonly)
		result.GetObject("bind").Set("create_host_path", create)
		return result
	}
	identityMount := func(readonly bool) *jsonx.Object {
		if identityMode == "file" {
			return bind(params.GetString("ha_host_id_path"), "/run/configflow-host-machine-id", true, false)
		}
		return jsonx.Obj("type", "volume", "source", "host-identity", "target", "/run/configflow-host-identity",
			"read_only", readonly, "volume", jsonx.Obj("nocopy", true))
	}
	host := params.Value("network_mode") == "host"
	for name, value := range compose.GetObject("services").All() {
		service := value.(*jsonx.Object)
		service.Set("image", image)
		service.Set("labels", jsonx.Obj(ComposeLabel, owner))
		helper := name == "ha-companion"
		volumes := []any{bind(data+"/agent", "/opt/configflow-agent", helper, true),
			bind(data+"/oxidns", "/etc/oxidns", helper, true),
			bind(data+"/ha", "/var/lib/configflow-ha", false, true), identityMount(true)}
		service.Set("volumes", volumes)
		env := jsonx.NewObject()
		for _, item := range service.GetList("environment") {
			entry, _ := item.(string)
			k, v, found := strings.Cut(entry, "=")
			if !found {
				return "", nil, valueError("dictionary update sequence element has length 1; 2 is required")
			}
			env.Set(k, v)
		}
		if identityMode == "docker-volume" {
			env.Set("CONFIGFLOW_DNS_HA_IDENTITY", "volume")
		}
		if name == "agent" {
			volumes = append(volumes, bind("/proc/sys/kernel/random/boot_id", "/run/configflow-host-boot-id", true, false))
			if host {
				volumes = append(volumes, bind("/proc/1/ns/net", "/run/configflow-host-netns", true, false))
			}
			service.Set("volumes", volumes)
			service.Set("entrypoint", []any{"python3", ComposeRuntime, "run"})
			service.Set("command", []any{})
			agentSettings := settings.Clone()
			agentSettings.Set("token", enrollmentToken(enrollment))
			encoded, err := jsonx.Encode(agentSettings, jsonx.Options{EnsureASCII: true, Compact: true})
			if err != nil {
				return "", nil, err
			}
			env.Set("CONFIGFLOW_COMPOSE_INSTALL", string(encoded))
			dependencies := jsonx.Obj("install-init", jsonx.Obj("condition", "service_completed_successfully"))
			if host {
				dependencies.Set("ha-companion", jsonx.Obj("condition", "service_started"))
			}
			service.Set("depends_on", dependencies)
		} else {
			service.Set("depends_on", jsonx.Obj("install-init", jsonx.Obj("condition", "service_completed_successfully")))
		}
		service.Set("environment", env)
	}
	initSettings, err := jsonx.Encode(settings, jsonx.Options{EnsureASCII: true, Compact: true})
	if err != nil {
		return "", nil, err
	}
	compose.GetObject("services").Set("install-init", jsonx.Obj(
		"image", image, "entrypoint", []any{"python3", ComposeRuntime, "prepare"}, "command", []any{},
		"restart", "no", "network_mode", "none", "read_only", true, "user", "0:0",
		"cap_drop", []any{"ALL"}, "cap_add", []any{"DAC_OVERRIDE", "FOWNER"},
		"security_opt", []any{"no-new-privileges:true"},
		"environment", jsonx.Obj("CONFIGFLOW_COMPOSE_INSTALL", string(initSettings), "PYTHONDONTWRITEBYTECODE", "1"),
		"volumes", []any{bind(data, "/configflow-data", false, true), identityMount(false)},
		"labels", jsonx.Obj(ComposeLabel, owner),
		"logging", jsonx.Obj("driver", "json-file", "options", jsonx.Obj("max-size", "5m", "max-file", "2")),
	))
	if identityMode == "docker-volume" {
		compose.Set("volumes", jsonx.Obj("host-identity", jsonx.Obj("name", ComposeIdentityVolume, "driver", "local")))
	}
	for _, value := range compose.GetObject("networks").All() {
		network := value.(*jsonx.Object)
		network.Set("name", project+"-dns-ha")
		network.Set("labels", jsonx.Obj(ComposeLabel, owner))
	}
	text, err := haDumpCompose(compose)
	if err != nil {
		return "", nil, err
	}
	return text, jsonx.Obj("filename", "compose.yaml", "project_name", project, "command", "docker compose up -d",
		"identity_mode", identityMode), nil
}
