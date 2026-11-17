// Package oxidns serves profile-scoped OxiDNS editor requests. Engine edits and
// detached persistence belong to internal/oxidns; this package translates HTTP.
package oxidns

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/model"
	engine "configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

type draftRequest struct {
	Settings         json.RawMessage `json:"settings"`
	BaseURL          json.RawMessage `json:"base_url"`
	NativeConfig     json.RawMessage `json:"native_config"`
	Changes          json.RawMessage `json:"changes"`
	TemplateSettings json.RawMessage `json:"template_settings"`
	Resolutions      json.RawMessage `json:"resolutions"`
	Renames          json.RawMessage `json:"renames"`
	StandardAdd      json.RawMessage `json:"standard_add"`
}

// UnmarshalJSON keeps request member names exact: encoding/json's default
// case-insensitive struct matching would turn unrelated fields into edit intent.
func (r *draftRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = draftRequest{
		Settings: fields["settings"], BaseURL: fields["base_url"],
		NativeConfig: fields["native_config"], Changes: fields["changes"],
		TemplateSettings: fields["template_settings"], Resolutions: fields["resolutions"],
		Renames: fields["renames"], StandardAdd: fields["standard_add"],
	}
	return nil
}

type commonResponse struct {
	Values       model.Dynamic                `json:"values"`
	Capabilities map[string]engine.Capability `json:"capabilities"`
	Targets      []engine.MatchTarget         `json:"custom_match_targets"`
	TargetTags   []string                     `json:"custom_match_target_tags"`
}
type appliedResponse struct {
	commonResponse
	Settings model.Dynamic `json:"settings"`
}
type nativeResponse struct {
	appliedResponse
	Notices []string `json:"notices"`
}

// Register installs all 18 method/path pairs, including their profile wrappers.
func Register(router *httpx.Router) {
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/<profile_id>/oxidns"} {
		blueprint := "oxidns."
		profilePrefix := ""
		if strings.Contains(prefix, "<profile_id>") {
			blueprint = "profiles."
			profilePrefix = "profile_"
		}
		router.Handle([]string{"GET", "POST"}, prefix+"/settings", blueprint+profilePrefix+"oxidns_settings", auth.Require(settings))
		for _, entry := range []struct{ path, endpoint string }{
			{"render", "render_oxidns_draft"}, {"validate", "validate_oxidns_draft"},
			{"editor", "prepare_oxidns_editor"}, {"template-preview", "preview_oxidns_template"},
			{"common-settings", "read_oxidns_common_settings"}, {"common-settings/apply", "apply_oxidns_common_settings"},
			{"native-settings/apply", "apply_oxidns_native_settings"},
		} {
			action := entry.path
			router.POST(prefix+"/"+action, blueprint+profilePrefix+entry.endpoint, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) { return draft(c, action) }))
		}
	}
}

func userFailure(err error, native bool) (*httpx.Response, error) {
	message, ok := engine.IsSettingsError(err)
	if !ok {
		return nil, err
	}
	value := struct {
		Success    bool               `json:"success"`
		Message    string             `json:"message"`
		References []engine.Reference `json:"references,omitempty"`
	}{Message: message}
	if native {
		var reference *engine.ReferenceError
		if errors.As(err, &reference) {
			value.References = reference.References
		}
	}
	response := httpx.JSONStatus(http.StatusBadRequest, value)
	if native {
		response.NoStore()
	}
	return response, nil
}
func settings(c *httpx.Ctx) (*httpx.Response, error) {
	if c.Method() == http.MethodGet || c.Method() == http.MethodHead {
		value, err := engine.ReadSettings(c.Context(), c.ProfileID)
		if err != nil {
			return userFailure(err, false)
		}
		return httpx.JSON(value.Settings).WithHeader("ETag", value.ETag), nil
	}
	var raw json.RawMessage
	if !c.BindSilent(&raw) {
		return httpx.Message(http.StatusBadRequest, false, engine.SettingsObjectMessage), nil
	}
	node, err := yamlx.FromJSON(raw)
	if err != nil {
		return httpx.Message(http.StatusBadRequest, false, engine.SettingsObjectMessage), nil
	}
	saved, err := engine.SaveSettings(c.Context(), c.ProfileID, node, c.HeaderDefault("If-Match", ""))
	if errors.Is(err, engine.ErrSettingsChanged) {
		return httpx.JSONStatus(http.StatusConflict, struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "oxidns_changed_elsewhere", Message: engine.SettingsChangedMessage}), nil
	}
	if err != nil {
		return userFailure(err, false)
	}
	return httpx.JSON(struct {
		Success  bool                `json:"success"`
		Settings model.Dynamic       `json:"settings"`
		Draft    bool                `json:"draft"`
		Guard    engine.PublishGuard `json:"publish_guard"`
	}{true, saved.Settings, true, saved.Guard}).WithHeader("ETag", saved.ETag), nil
}
func object(node *yamlx.Node) bool { return node != nil && node.Kind == yaml.MappingNode }
func readDraft(c *httpx.Ctx) (draftRequest, *engine.Node, string, error) {
	var request draftRequest
	if !c.BindSilent(&request) {
		return request, nil, "", &engine.SettingsError{Msg: messageSettingsRequired}
	}
	node, err := yamlx.FromJSON(request.Settings)
	if err != nil || !object(node) {
		return request, nil, "", &engine.SettingsError{Msg: messageSettingsRequired}
	}
	base := strings.TrimRight(c.URLRoot(), "/")
	if len(request.BaseURL) > 0 {
		value, err := yamlx.FromJSON(request.BaseURL)
		if err != nil || value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
			return request, nil, "", &engine.SettingsError{Msg: messageBaseURLText}
		}
		base = value.Value
	}
	return request, node, base, nil
}
func optionalNode(raw json.RawMessage) (*engine.Node, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return yamlx.FromJSON(raw)
}
func dynamic(node *engine.Node) (model.Dynamic, error) {
	var value model.Dynamic
	err := value.SetNode(node)
	return value, err
}
func common(view engine.CommonView) (commonResponse, error) {
	values, err := dynamic(view.Values)
	return commonResponse{Values: values, Capabilities: view.Capabilities, Targets: view.MatchTargets.Targets, TargetTags: view.MatchTargets.Tags}, err
}
func applied(value engine.Applied) (appliedResponse, error) {
	view, err := common(value.View)
	if err != nil {
		return appliedResponse{}, err
	}
	settings, err := dynamic(value.Settings)
	return appliedResponse{commonResponse: view, Settings: settings}, err
}
func draft(c *httpx.Ctx, action string) (*httpx.Response, error) {
	request, settings, baseURL, err := readDraft(c)
	native := action == "native-settings/apply"
	if err != nil {
		return userFailure(err, native)
	}
	config, err := repo.GetConfig(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	profile := engine.NewProfile(config)
	switch action {
	case "render":
		normalized, err := engine.NormalizeSettings(settings, true)
		if err != nil {
			return userFailure(err, false)
		}
		config = config.DeepCopy()
		config.Set("oxidns", engine.ToJSONX(normalized))
		text, err := engine.Generate(engine.NewProfile(config), baseURL)
		if err != nil {
			return userFailure(err, false)
		}
		return httpx.JSON(struct {
			Config string `json:"config"`
		}{text}).NoStore(), nil
	case "validate":
		result, err := engine.ValidateSettings(settings, profile, baseURL)
		if err != nil {
			return userFailure(err, false)
		}
		return httpx.JSON(result).NoStore(), nil
	case "editor":
		result, err := engine.PrepareEditor(settings, profile, baseURL)
		if err != nil {
			return userFailure(err, false)
		}
		value, err := dynamic(result)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(struct {
			Settings model.Dynamic `json:"settings"`
		}{value}).NoStore(), nil
	case "template-preview":
		template, err := optionalNode(request.TemplateSettings)
		if err != nil {
			return nil, err
		}
		resolutions, err := optionalNode(request.Resolutions)
		if err != nil {
			return nil, err
		}
		result, err := engine.PreviewTemplate(settings, template, profile, baseURL, resolutions)
		if err != nil {
			return userFailure(err, false)
		}
		value, err := dynamic(result.Settings)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(struct {
			Settings  model.Dynamic     `json:"settings"`
			Changes   []engine.Change   `json:"changes"`
			Conflicts []engine.Conflict `json:"conflicts"`
		}{value, result.Changes, result.Conflicts}).NoStore(), nil
	case "common-settings":
		result, err := engine.ReadCommonSettings(settings, profile, baseURL)
		if err != nil {
			return userFailure(err, false)
		}
		value, err := common(result)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(value).NoStore(), nil
	case "common-settings/apply":
		changes, err := optionalNode(request.Changes)
		if err != nil {
			return nil, err
		}
		result, err := engine.ApplyCommonSettings(settings, changes, profile, baseURL)
		if err != nil {
			return userFailure(err, false)
		}
		value, err := applied(result)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(value).NoStore(), nil
	case "native-settings/apply":
		text, err := optionalNode(request.NativeConfig)
		if err != nil || text == nil || text.Kind != yaml.ScalarNode || text.ShortTag() != "!!str" {
			return userFailure(&engine.SettingsError{Msg: engine.NativeConfigTextMessage}, true)
		}
		prepared, err := engine.PrepareNativeDraft(settings, text.Value, profile, baseURL)
		if err != nil {
			return userFailure(err, true)
		}
		options, err := engine.DecodeNativeOptions(request.Renames, request.StandardAdd)
		if err != nil {
			return userFailure(err, true)
		}
		result, err := prepared.Apply(options)
		if err != nil {
			return userFailure(err, true)
		}
		value, err := applied(engine.Applied{Settings: result.Settings, View: result.View})
		if err != nil {
			return nil, err
		}
		return httpx.JSON(nativeResponse{appliedResponse: value, Notices: result.Notices}).NoStore(), nil
	}
	return nil, httpx.BadRequest("")
}
