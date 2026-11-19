package installer

import (
	"encoding/json"
	"errors"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// ValueError is the compatibility name for ValidationError.
type ValueError = ValidationError

// TypeError is the compatibility name for RenderError.
type TypeError = RenderError

// Enrollment is the compatibility response for callers not yet using Capability.
type Enrollment struct {
	URL       string
	Token     string
	ExpiresAt any
}

func nativeCapability(e *Enrollment) *Capability {
	if e == nil {
		return nil
	}
	return &Capability{URL: e.URL, Token: e.Token}
}

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
	result, err := GenerateGuideJSON(raw, nativeCapability(enrollment))
	if err != nil {
		return nil, err
	}
	return legacyResponse(result)
}

// IdentityCommand renders the bounded host-identity creation step.
func IdentityCommand(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(msgIdentityPathCanonical)
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
		return "", valueError(msgOriginalContainerName)
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
	p := GuideOptions{identity: fields}
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"server_url", &p.ServerURL}, {"agent_name", &p.AgentName}, {"agent_ip", &p.AgentIP}, {"data_dir", &p.DataDir}, {"network_mode", &p.NetworkMode}, {"ha_parent", &p.HAParent}, {"ha_subnet", &p.HASubnet}, {"ha_gateway", &p.HAGateway}, {"ha_host_id_path", &p.HAHostIDPath},
	} {
		*field.target, _ = fieldText(fields, field.key)
	}
	p.OxidnsPort, _ = fieldNumber(fields, "oxidns_port")
	addressRaw, _ := fields.Get("agent_ip")
	if address, node, display, ok := decodeIPv4Value(addressRaw); ok {
		p.AgentIP = address
		p.addressNode = node
		p.addressDisplay = display
	}
	gatewayRaw, _ := fields.Get("ha_gateway")
	if emptyInspectionName(gatewayRaw) {
		p.HAGateway = ""
	} else if gateway, _, _, ok := decodeIPv4Value(gatewayRaw); ok {
		p.HAGateway = gateway
	} else {
		p.HAGateway = "invalid-gateway"
	}
	r := GuideRequest{Options: p, InstallMethod: method, Image: DockerHAImage}
	if request != nil {
		r.CreateHostIdentity = jsonx.Truthy(request.GetOr("create_host_identity", nil))
		if image, present := request.Get("image"); present {
			r.Image, _ = image.(string)
			r.imageRaw, err = jsonx.Default(image)
			if err != nil {
				return GuideOptions{}, GuideRequest{}, err
			}
			p.imageRaw = r.imageRaw
		}
	}
	r.Options = p
	return p, r, nil
}

// GenerateCompact adapts the legacy renderer arguments to a typed plan.
func GenerateCompact(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	p, r, err := legacyGuideInputs(params, request, "compose_compact")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateCompactNative(p, r, nativeCapability(enrollment))
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
	text, info, err := generateDirectNative(p, r, nativeCapability(enrollment))
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
	text, info, err := generateBundleNative(p, r, nativeCapability(enrollment))
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
	return bootstrapNative(nativeCapability(enrollment), GuideRequest{Options: p, Image: image})
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
