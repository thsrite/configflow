package difftest

// A10 part of the Docker HA differential: the Compose install methods
// (compose bundle, compose_direct, compose_compact). Their answers embed the
// side's own random capability (and hashes of it: project names, owner), so
// each side's answer is compared with installer.GenerateGuideJSON run in this
// process with the capability read back from that answer: Python's answer
// against the Go generators given Python's capability (byte-for-byte,
// including the bundle's zip), Go's answer against the same generators given
// Go's capability (the route wiring).

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"configflow/internal/installer"
	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// composeEnrollment reads the capability a Compose answer carries.
func composeEnrollment(method string, answer, body *jsonx.Object) (*installer.Capability, error) {
	environment := func(key string) (string, error) {
		node, err := yamlx.Parse([]byte(answer.GetString("compose")))
		if err != nil {
			return "", err
		}
		for _, part := range []string{"services", "agent", "environment", key} {
			node = yamlx.Get(node, part)
		}
		var value string
		if err := yamlx.Decode(node, &value); err != nil {
			return "", err
		}
		return strings.ReplaceAll(value, "$$", "$"), nil
	}
	switch method {
	case "compose":
		archive, err := base64.StdEncoding.DecodeString(answer.GetObject("compose_bundle").GetString("content_base64"))
		if err != nil {
			return nil, err
		}
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if file.Name != "enrollment.json" {
				continue
			}
			stream, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, _ := io.ReadAll(stream)
			stream.Close()
			value, err := jsonx.Parse(data)
			if err != nil {
				return nil, err
			}
			settings := value.(*jsonx.Object)
			return &installer.Capability{URL: settings.GetString("url"), Token: settings.GetString("token")}, nil
		}
		return nil, fmt.Errorf("no enrollment.json in the bundle")
	case "compose_direct":
		text, err := environment("CONFIGFLOW_COMPOSE_INSTALL")
		if err != nil {
			return nil, err
		}
		value, err := jsonx.Parse([]byte(text))
		if err != nil {
			return nil, err
		}
		settings := value.(*jsonx.Object)
		return &installer.Capability{URL: settings.GetString("url"), Token: settings.GetString("token")}, nil
	default:
		key, err := environment("INSTALL_KEY")
		if err != nil {
			return nil, err
		}
		session, token, found := strings.Cut(key, ".")
		if !found {
			return nil, fmt.Errorf("INSTALL_KEY %q", key)
		}
		server := strings.TrimRight(body.GetString("server_url"), "/")
		return &installer.Capability{URL: server + "/api/agents/docker-oxidns-ha-enroll/" + session, Token: token}, nil
	}
}

// compareComposeAnswer checks one side's Compose answer against the Go
// generators given that side's capability.
func compareComposeAnswer(side string, body *jsonx.Object, r response) []string {
	if r.status != 200 {
		return []string{fmt.Sprintf("%s status %d: %.300s", side, r.status, r.body)}
	}
	parsed, err := jsonx.Parse(r.body)
	if err != nil {
		return []string{side + " body: " + err.Error()}
	}
	answer := parsed.(*jsonx.Object)
	method := body.GetString("install_method")
	enrollment, err := composeEnrollment(method, answer, body)
	if err != nil {
		return []string{side + " capability: " + err.Error()}
	}
	raw, err := jsonx.Default(body)
	if err != nil {
		return []string{side + " request: " + err.Error()}
	}
	result, err := installer.GenerateGuideJSON(raw, enrollment)
	if err != nil {
		return []string{side + " guide: " + err.Error()}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return []string{side + " response: " + err.Error()}
	}
	value, err := jsonx.Parse(encoded)
	expected, ok := value.(*jsonx.Object)
	if err == nil && !ok {
		return []string{side + " non-object guide"}
	}
	if err != nil {
		return []string{fmt.Sprintf("%s: go generators refused the request: %v", side, err)}
	}
	// routes/docker_ha_installation.py adds the expiry and one warning.
	expected.Set("enrollment_expires_at", answer.Value("enrollment_expires_at"))
	expected.Set("warnings", append(expected.GetList("warnings"), "新装命令的本机安装凭据有效期为 24 小时；过期后重新生成。生成命令不会创建设备记录。"))
	expectedBytes, encodeErr := jsonx.Response(expected)
	if encodeErr != nil {
		return []string{side + " expected: " + encodeErr.Error()}
	}
	if side == "python" {
		expectedBytes, err = installertest.GuideBaseline(expectedBytes)
		if err != nil {
			return []string{side + " approved HA delta: " + err.Error()}
		}
	}
	normalized := equalGuideComposeLayout(expectedBytes, r.body)
	parsedExpected, parseErr := jsonx.Parse(normalized)
	if parseErr != nil {
		return []string{side + " expected: " + parseErr.Error()}
	}
	expected = parsedExpected.(*jsonx.Object)
	left, leftErr := installerJSONValues(expected)
	right, rightErr := installerJSONValues(answer)
	if leftErr == nil && rightErr == nil && jsonx.Equal(left, right) {
		return nil
	}
	var diffs []string
	for _, key := range answer.Keys() {
		if !jsonx.Equal(answer.Value(key), expected.Value(key)) {
			a, b := jsonx.Repr(answer.Value(key)), jsonx.Repr(expected.Value(key))
			diffs = append(diffs, fmt.Sprintf("%s key %s: answer %.300s generators %.300s", side, key, a, b))
		}
	}
	if len(answer.Keys()) != len(expected.Keys()) {
		diffs = append(diffs, fmt.Sprintf("%s keys answer %v generators %v", side, answer.Keys(), expected.Keys()))
	}
	return diffs
}

// composeBodies are the Compose requests of the differential.
func composeBodies() []*jsonx.Object {
	var out []*jsonx.Object
	for _, method := range []string{"compose", "compose_direct", "compose_compact"} {
		out = append(out,
			haNewBody("install_method", method, "agent_name", method),
			haNewBody("install_method", method, "agent_name", method+"-mv", "network_mode", "macvlan", "ha_parent", "eth0",
				"ha_subnet", "192.168.50.0/24", "ha_gateway", "192.168.50.1", "data_dir", "/srv/oxidns-mv"),
			haNewBody("install_method", method, "agent_name", method+"-id", "create_host_identity", true, "data_dir", "/srv/oxidns-id",
				"ha_host_id_path", "/srv/ids/host-id"),
			haNewBody("install_method", method, "agent_name", method+"-odd", "server_url", "https://panel.example/it's/$(id)/",
				"data_dir", "/srv/目录 $data `x`", "image", "local.test/agent:compose"),
			haNewBody("install_method", method, "agent_name", method+"-int", "agent_ip", 3232248340,
				"data_dir", "/srv/oxidns-int", "oxidns_port", 8083),
		)
	}
	return out
}
