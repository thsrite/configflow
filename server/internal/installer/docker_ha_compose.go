package installer

// The self-contained, new-install-only Compose package (compose.yaml,
// prepare.sh, enrollment.json, README.txt in a zip) using a scoped
// enrollment; the agent service starts with `configflow-agent ha-bootstrap`
// from the Agent image. The literal script/README text lives in
// docker_ha_compose_text.go.

import (
	"encoding/base64"
	"path"
	"strings"

	"encoding/json"
)

// ComposeLabel is LABEL.
const ComposeLabel = "io.configflow.dns-ha.installation"

func enrollmentToken(e *Capability) string {
	if e == nil {
		return ""
	}
	return e.Token
}

// composePrepare is _prepare(params, request, owner, project, compose).
func composePrepare(params GuideOptions, request GuideRequest, owner, project string, compose *composeDocument) (string, error) {
	data := params.DataDir
	state := data + "/.ha-compose"
	identityPath := params.HAHostIDPath
	script := haHeader() + composePrepareBlocks[0]
	script += haDaemonCheck()
	script += composePrepareBlocks[1]
	script += "cf_data=" + q(data) + "\ncf_state=" + q(state) + "\ncf_owner=" + q(owner) + "\ncf_project=" + q(project) + "\ncf_identity_source=" + q(identityPath) + "\n"
	script += "cf_no_links \"$cf_data\"\ncf_no_links " + q(identityPath) + "\n"
	script += composePrepareBlocks[2]
	for key, service := range compose.Services.all() {
		containerName := service.ContainerName
		name := q(containerName)
		script += "if docker container inspect " + name + " >/dev/null 2>&1; then\n"
		script += "  [ \"$cf_known\" = true ] || " + haFail("同名容器已存在，拒绝接管："+containerName) + "\n"
		checks := [][2]string{{`{{index .Config.Labels "` + ComposeLabel + `"}}`, owner},
			{`{{index .Config.Labels "com.docker.compose.project"}}`, project},
			{`{{index .Config.Labels "com.docker.compose.service"}}`, key}}
		for _, mount := range service.Volumes {
			target, err := json.Marshal(mount.Target)
			if err != nil {
				return "", err
			}
			rw := "true"
			if mount.ReadOnly {
				rw = "false"
			}
			format := `{{range .Mounts}}{{if eq .Destination ` + string(target) + `}}{{.Type}}|{{.Source}}|{{.RW}}{{end}}{{end}}`
			checks = append(checks, [2]string{format, "bind|" + mount.Source + "|" + rw})
		}
		for _, check := range checks {
			script += "  [ \"$(docker container inspect --format " + q(check[0]) + " " + name + ")\" = " + q(check[1]) + " ] || " + haFail("现有容器的项目、归属或挂载与本次安装不符；未修改容器") + "\n"
		}
		script += "fi\n"
	}
	for _, network := range compose.Networks {
		name := q(network.Name)
		script += "if docker network inspect " + name + " >/dev/null 2>&1; then\n"
		script += "  [ \"$cf_known\" = true ] || " + haFail("同名网络已存在，拒绝接管") + "\n"
		ipam := network.IPAM.Config[0]
		parent, subnet := network.DriverOpts.Parent, ipam.Subnet
		checks := [][2]string{{"{{.Driver}}", "macvlan"}, {`{{index .Options "parent"}}`, parent},
			{`{{index .Labels "` + ComposeLabel + `"}}`, owner},
			{`{{index .Labels "com.docker.compose.project"}}`, project},
			{"{{len .IPAM.Config}}", "1"}, {"{{(index .IPAM.Config 0).Subnet}}", subnet}}
		if ipam.Gateway != "" {
			checks = append(checks, [2]string{"{{(index .IPAM.Config 0).Gateway}}", ipam.Gateway})
		}
		for _, check := range checks {
			script += "  [ \"$(docker network inspect --format " + q(check[0]) + " " + name + ")\" = " + q(check[1]) + " ] || " + haFail("现有网络参数或项目归属不符，拒绝复用") + "\n"
		}
		script += "fi\n"
	}
	if request.CreateHostIdentity {
		script += "if [ ! -e " + q(identityPath) + " ] && [ ! -L " + q(identityPath) + " ]; then\n"
		command, err := identityCommand(identityPath)
		if err != nil {
			return "", err
		}
		script += strings.TrimPrefix(command, haHeader()) + "fi\n"
	}
	script += haIdentityCheck(identityPath)
	script += composePrepareBlocks[3]
	script += composePrepareBlocks[4]
	return script, nil
}

// composeManagedParts are the data_dir children the host identity file may
// not alias (generate_bundle). bootstrap.py stays reserved: bundles before
// Agent 1.9.2-go kept it in .ha-compose and the data root.
var composeManagedParts = []string{"agent", "oxidns", "ha", ".ha-compose", ".ha-install",
	"compose.yaml", "prepare.sh", "bootstrap.py", "enrollment.json", "README.txt"}

// GenerateBundle is generate_bundle(params, request, enrollment): the
// Compose text and the bundle dict (filename, project_name, content_base64,
// prepare_command, start_command).
func generateBundleNative(params GuideOptions, request GuideRequest, enrollment *Capability) (string, *ComposeBundle, error) {
	image := request.Image
	owner, err := planHash(params, map[string]string{"image": image, "enrollment_url": enrollmentURL(enrollment), "install_method": "compose"}, true)
	if err != nil {
		return "", nil, err
	}
	project := "cf-oxidns-" + owner[:16]
	data := params.DataDir
	state := data + "/.ha-compose"
	if raw, present := params.identity.Get("ha_host_id_path"); present && len(raw) > 0 && raw[0] != '"' {
		return "", nil, typeError(msgRendererText)
	}
	identity := params.HAHostIDPath
	identityCheck, dataCheck := path.Clean(identity), path.Clean(data)
	aliased := identityCheck == dataCheck
	for _, part := range composeManagedParts {
		if identityCheck == dataCheck+"/"+part || strings.HasPrefix(identityCheck, dataCheck+"/"+part+"/") {
			aliased = true
		}
	}
	if aliased {
		return "", nil, valueError(msgBundleIdentityLocation)
	}
	compose, err := guideComposeNative(params)
	if err != nil {
		return "", nil, err
	}
	compose.Version = ""
	compose.Name = project
	for key, service := range compose.Services.all() {
		service.Image = imageNode(request)
		service.Labels = map[string]string{ComposeLabel: owner}
		readonly := key == "ha-companion"
		volumes := []composeMount{bindMount(data+"/agent", "/opt/configflow-agent", readonly, false), bindMount(data+"/oxidns", "/etc/oxidns", readonly, false), bindMount(data+"/ha", "/var/lib/configflow-ha", false, false), bindMount(identity, "/run/configflow-host-machine-id", true, false)}
		if key == "agent" {
			if params.NetworkMode == "host" {
				volumes = append(volumes, bindMount("/proc/1/ns/net", "/run/configflow-host-netns", true, false))
			}
			volumes = append(volumes, bindMount(state, "/run/configflow-compose", true, false))
			service.Entrypoint = []string{"configflow-agent", "ha-bootstrap"}
			empty := []string{}
			service.Command = &empty
		}
		service.Volumes = volumes
	}
	for _, network := range compose.Networks {
		network.Name = project + "-dns-ha"
		network.Labels = map[string]string{ComposeLabel: owner}
	}
	settings := struct {
		Owner string `json:"owner"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}{owner, enrollmentURL(enrollment), enrollmentToken(enrollment)}
	content, err := haDumpCompose(compose)
	if err != nil {
		return "", nil, err
	}
	prepare, err := composePrepare(params, request, owner, project, compose)
	if err != nil {
		return "", nil, err
	}
	settingsJSON, err := renderConfigJSON(settings)
	if err != nil {
		return "", nil, err
	}
	readme := composeReadme[0] + composeReadme[1] + project + composeReadme[2]
	archive, err := composeArchive([]zipEntry{
		{"compose.yaml", []byte(content), 0o600},
		{"prepare.sh", []byte(prepare), 0o700},
		{"enrollment.json", append(settingsJSON, '\n'), 0o600},
		{"README.txt", []byte(readme), 0o600},
	})
	if err != nil {
		return "", nil, err
	}
	return content, &ComposeBundle{Filename: project + ".zip", ProjectName: project, ContentBase64: base64.StdEncoding.EncodeToString(archive), PrepareCommand: "sudo bash prepare.sh", StartCommand: "docker compose up -d"}, nil
}
