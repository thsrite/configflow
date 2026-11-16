package installer

// Port of backend/agents/docker_ha_compose.py (module A10): the
// self-contained, new-install-only Compose package (compose.yaml,
// prepare.sh, bootstrap.py, enrollment.json, README.txt in a zip) using a
// scoped enrollment. The literal script/README text lives in the generated
// docker_ha_compose_text.go.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"configflow/internal/jsonx"
)

// ComposeLabel is LABEL.
const ComposeLabel = "io.configflow.dns-ha.installation"

func init() {
	generateCompact = GenerateCompact
	generateDirect = GenerateDirect
	generateBundle = func(params, request *jsonx.Object, enrollment *Enrollment) (string, any, error) {
		return GenerateBundle(params, request, enrollment)
	}
}

// pyJSONHash is hashlib.sha256(json.dumps(value, sort_keys=True,
// separators=...).encode()).hexdigest() (ensure_ascii=True).
func pyJSONHash(value any, compact bool) (string, error) {
	data, err := jsonx.Encode(value, jsonx.Options{EnsureASCII: true, SortKeys: true, Compact: compact})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// pyConcat is the str + str of the Python generators: a TypeError for a
// value that is not a str.
func pyConcat(value any) (string, error) {
	text, isString := value.(string)
	if !isString {
		return "", typeError("can only concatenate str (not \"" + pyTypeName(value) + "\") to str")
	}
	return text, nil
}

func enrollmentToken(e *Enrollment) string {
	if e == nil {
		return ""
	}
	return e.Token
}

// requestImage is request.get('image', 'thsrite/config-flow-agent:latest').
func requestImage(request *jsonx.Object) any {
	return request.GetOr("image", DockerHAImage)
}

// composePrepare is _prepare(params, request, owner, project, compose).
func composePrepare(params, request *jsonx.Object, owner, project string, compose *jsonx.Object) (string, error) {
	data := params.GetString("data_dir")
	state := data + "/.ha-compose"
	identityPath := params.GetString("ha_host_id_path")
	script := haHeader() + composePrepareBlocks[0]
	script += haDaemonCheck()
	script += composePrepareBlocks[1]
	script += "cf_data=" + q(data) + "\ncf_state=" + q(state) + "\ncf_owner=" + q(owner) + "\ncf_project=" + q(project) + "\ncf_identity_source=" + q(identityPath) + "\n"
	script += "cf_no_links \"$cf_data\"\ncf_no_links " + q(identityPath) + "\n"
	script += composePrepareBlocks[2]
	for key, value := range compose.GetObject("services").All() {
		service := value.(*jsonx.Object)
		containerName, err := pyConcat(service.Value("container_name"))
		if err != nil {
			return "", err
		}
		name := q(containerName)
		script += "if docker container inspect " + name + " >/dev/null 2>&1; then\n"
		script += "  [ \"$cf_known\" = true ] || " + haFail("同名容器已存在，拒绝接管："+containerName) + "\n"
		checks := [][2]string{{`{{index .Config.Labels "` + ComposeLabel + `"}}`, owner},
			{`{{index .Config.Labels "com.docker.compose.project"}}`, project},
			{`{{index .Config.Labels "com.docker.compose.service"}}`, key}}
		for _, item := range service.GetList("volumes") {
			mount := item.(*jsonx.Object)
			target, err := jsonx.Default(mount.Value("target"))
			if err != nil {
				return "", err
			}
			rw := "true"
			if jsonx.Truthy(mount.GetOr("read_only", nil)) {
				rw = "false"
			}
			format := `{{range .Mounts}}{{if eq .Destination ` + string(target) + `}}{{.Type}}|{{.Source}}|{{.RW}}{{end}}{{end}}`
			checks = append(checks, [2]string{format, "bind|" + mount.GetString("source") + "|" + rw})
		}
		for _, check := range checks {
			script += "  [ \"$(docker container inspect --format " + q(check[0]) + " " + name + ")\" = " + q(check[1]) + " ] || " + haFail("现有容器的项目、归属或挂载与本次安装不符；未修改容器") + "\n"
		}
		script += "fi\n"
	}
	for _, value := range compose.GetObject("networks").All() {
		network := value.(*jsonx.Object)
		name := q(network.GetString("name"))
		script += "if docker network inspect " + name + " >/dev/null 2>&1; then\n"
		script += "  [ \"$cf_known\" = true ] || " + haFail("同名网络已存在，拒绝接管") + "\n"
		ipam := network.GetObject("ipam").GetList("config")[0].(*jsonx.Object)
		parent, err := pyConcat(network.GetObject("driver_opts").Value("parent"))
		if err != nil {
			return "", err
		}
		subnet, err := pyConcat(ipam.Value("subnet"))
		if err != nil {
			return "", err
		}
		checks := [][2]string{{"{{.Driver}}", "macvlan"}, {`{{index .Options "parent"}}`, parent},
			{`{{index .Labels "` + ComposeLabel + `"}}`, owner},
			{`{{index .Labels "com.docker.compose.project"}}`, project},
			{"{{len .IPAM.Config}}", "1"}, {"{{(index .IPAM.Config 0).Subnet}}", subnet}}
		if gateway := ipam.GetOr("gateway", nil); jsonx.Truthy(gateway) {
			text, err := pyConcat(gateway)
			if err != nil {
				return "", err
			}
			checks = append(checks, [2]string{"{{(index .IPAM.Config 0).Gateway}}", text})
		}
		for _, check := range checks {
			script += "  [ \"$(docker network inspect --format " + q(check[0]) + " " + name + ")\" = " + q(check[1]) + " ] || " + haFail("现有网络参数或项目归属不符，拒绝复用") + "\n"
		}
		script += "fi\n"
	}
	if jsonx.Truthy(request.GetOr("create_host_identity", nil)) {
		script += "if [ ! -e " + q(identityPath) + " ] && [ ! -L " + q(identityPath) + " ]; then\n"
		command, err := IdentityCommand(identityPath)
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
// not alias (generate_bundle).
var composeManagedParts = []string{"agent", "oxidns", "ha", ".ha-compose", ".ha-install",
	"compose.yaml", "prepare.sh", "bootstrap.py", "enrollment.json", "README.txt"}

// GenerateBundle is generate_bundle(params, request, enrollment): the
// Compose text and the bundle dict (filename, project_name, content_base64,
// prepare_command, start_command).
func GenerateBundle(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	image := requestImage(request)
	plan := params.Clone()
	plan.Set("image", image)
	plan.Set("enrollment_url", enrollmentURL(enrollment))
	plan.Set("install_method", "compose")
	owner, err := pyJSONHash(plan, true)
	if err != nil {
		return "", nil, err
	}
	project := "cf-oxidns-" + owner[:16]
	data := params.GetString("data_dir")
	state := data + "/.ha-compose"
	identity, err := pyConcat(params.Value("ha_host_id_path")) // identity.startswith: AttributeError
	if err != nil {
		return "", nil, err
	}
	aliased := identity == data
	for _, part := range composeManagedParts {
		if identity == data+"/"+part || strings.HasPrefix(identity, data+"/"+part+"/") {
			aliased = true
		}
	}
	if aliased {
		return "", nil, valueError("宿主身份文件不能放在本实例的受管数据子目录内，请选择独立持久文件")
	}
	compose, err := guideCompose(params)
	if err != nil {
		return "", nil, err
	}
	compose.Delete("version")
	compose.Set("name", project)
	for key, value := range compose.GetObject("services").All() {
		service := value.(*jsonx.Object)
		service.Set("image", image)
		service.Set("labels", jsonx.Obj(ComposeLabel, owner))
		readonly := key == "ha-companion"
		bind := func(source string) *jsonx.Object { return jsonx.Obj("type", "bind", "source", source) }
		volumes := []any{guideVolume(bind(data+"/agent"), "/opt/configflow-agent", readonly),
			guideVolume(bind(data+"/oxidns"), "/etc/oxidns", readonly),
			guideVolume(bind(data+"/ha"), "/var/lib/configflow-ha", false),
			guideVolume(bind(identity), "/run/configflow-host-machine-id", true)}
		if key == "agent" {
			if params.Value("network_mode") == "host" {
				volumes = append(volumes, guideVolume(bind("/proc/1/ns/net"), "/run/configflow-host-netns", true))
			}
			volumes = append(volumes, guideVolume(bind(state), "/run/configflow-compose", true))
			service.Set("volumes", volumes)
			service.Set("entrypoint", []any{"python3", "/run/configflow-compose/bootstrap.py"})
			service.Set("command", []any{})
		} else {
			service.Set("volumes", volumes)
		}
	}
	for _, value := range compose.GetObject("networks").All() {
		network := value.(*jsonx.Object)
		network.Set("name", project+"-dns-ha")
		network.Set("labels", jsonx.Obj(ComposeLabel, owner))
	}
	settings := jsonx.Obj("owner", owner, "url", enrollmentURL(enrollment), "token", enrollmentToken(enrollment))
	content, err := haDumpCompose(compose)
	if err != nil {
		return "", nil, err
	}
	prepare, err := composePrepare(params, request, owner, project, compose)
	if err != nil {
		return "", nil, err
	}
	settingsJSON, err := jsonx.Encode(settings, jsonx.Options{EnsureASCII: true, Indent: 2})
	if err != nil {
		return "", nil, err
	}
	readme := composeReadme[0] + composeReadme[1] + project + composeReadme[2]
	archive := pyZipArchive([]zipEntry{
		{"compose.yaml", []byte(content), 0o600},
		{"prepare.sh", []byte(prepare), 0o700},
		{"bootstrap.py", []byte(composeBootstrap), 0o600},
		{"enrollment.json", append(settingsJSON, '\n'), 0o600},
		{"README.txt", []byte(readme), 0o600},
	})
	return content, jsonx.Obj("filename", project+".zip", "project_name", project,
		"content_base64", base64.StdEncoding.EncodeToString(archive),
		"prepare_command", "sudo bash prepare.sh", "start_command", "docker compose up -d"), nil
}
