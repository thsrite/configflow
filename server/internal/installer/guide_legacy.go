package installer

import (
	"encoding/json"
	"errors"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func legacyResponse(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return decoded.(*jsonx.Object), nil
}

// GenerateGuide is the legacy facade for GenerateGuideJSON.
func GenerateGuide(value any, enrollment *Enrollment) (*jsonx.Object, error) {
	raw, err := jsonx.Default(value)
	if err != nil {
		return nil, err
	}
	result, err := GenerateGuideJSON(raw, enrollment)
	if err != nil {
		return nil, err
	}
	return legacyResponse(result)
}

// IdentityCommand renders the bounded host-identity creation step.
func IdentityCommand(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError("宿主身份文件须为规范的绝对路径，不能包含逗号、冒号或换行")
	}
	return identityCommand(text)
}

// InspectionCommand renders read-only Docker inspection.
func InspectionCommand(value any) (string, error) {
	if !jsonx.Truthy(value) {
		return inspectionCommand("")
	}
	text, ok := value.(string)
	if !ok {
		return "", valueError("原容器名只能包含字母、数字、点、下划线或短横线")
	}
	return inspectionCommand(text)
}

func legacyGuideInputs(params, request *jsonx.Object, method string) (GuideOptions, GuideRequest, error) {
	raw, err := jsonx.Default(params)
	if err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	if request != nil {
		for _, key := range []string{"image", "create_host_identity"} {
			if value, present := request.Get(key); present {
				raw, err := jsonx.Default(value)
				if err != nil {
					return GuideOptions{}, GuideRequest{}, err
				}
				if err := fields.Set(key, raw); err != nil {
					return GuideOptions{}, GuideRequest{}, err
				}
			}
		}
	}
	if err := setSessionField(&fields, "install_method", method); err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	decoded, err := DecodeGuideRequest(raw)
	return decoded.Options, decoded, err
}

// GenerateCompact adapts the legacy renderer arguments to a typed plan.
func GenerateCompact(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	p, r, err := legacyGuideInputs(params, request, "compose_compact")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateCompactNative(p, r, enrollment)
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// GenerateDirect adapts the legacy renderer arguments to a typed plan.
func GenerateDirect(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	p, r, err := legacyGuideInputs(params, request, "compose_direct")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateDirectNative(p, r, enrollment)
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// GenerateBundle adapts the legacy renderer arguments to a typed plan.
func GenerateBundle(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	p, r, err := legacyGuideInputs(params, request, "compose")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateBundleNative(p, r, enrollment)
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// BootstrapCommand adapts legacy guide inputs to the image bootstrap command.
func BootstrapCommand(enrollment *Enrollment, request *jsonx.Object) (string, error) {
	raw, err := jsonx.Default(request)
	if err != nil {
		return "", err
	}
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	image, _ := textOr(fields, "image", DockerHAImage)
	p := GuideOptions{}
	p.DataDir, _ = fieldText(fields, "data_dir")
	p.AgentName, _ = textOr(fields, "agent_name", "agent")
	p.AgentIP, _ = fieldText(fields, "agent_ip")
	p.NetworkMode, _ = fieldText(fields, "network_mode")
	return bootstrapNative(enrollment, GuideRequest{Options: p, Image: image})
}

func annotateCompose(text string, compose *jsonx.Object) (string, error) {
	raw, err := jsonx.Default(compose)
	if err != nil {
		return "", err
	}
	node, err := yamlx.FromJSON(raw)
	if err != nil {
		return "", err
	}
	return annotateComposeNative(text, node)
}

// ErrComposeVariantNotPorted is retained for source compatibility; all variants
// are implemented by the typed renderer.
var ErrComposeVariantNotPorted = errors.New("Docker HA Compose variant is unavailable")
