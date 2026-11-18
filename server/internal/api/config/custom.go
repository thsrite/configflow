package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type customRequest struct {
	Config      json.RawMessage
	SmartGroups json.RawMessage
	Interval    json.RawMessage
	EasyTier    json.RawMessage
}

func (r *customRequest) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("configuration object expected")
	}
	*r = customRequest{fields["config"], fields["smart_groups"], fields["provider_update_interval"], fields["easytier"]}
	return nil
}
func registerCustom(r *httpx.Router) {
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		r.Handle([]string{"GET", "POST"}, "/api/custom-config/"+engine, "custom_config.handle_custom_"+engine+"_config", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			if c.Method() != "POST" {
				out, err := readCustom(c.Context(), c.ProfileID, engine)
				if err != nil {
					return nil, err
				}
				return httpx.JSON(out), nil
			}
			var body customRequest
			if engine == "oxidns" {
				if !c.BindSilent(&body) {
					return httpx.Message(400, false, msgYAMLText), nil
				}
			} else {
				if err := c.Bind(&body); err != nil {
					return nil, err
				}
			}
			guard, err := saveCustom(c.Context(), c.ProfileID, engine, body)
			if err != nil {
				var failure *customError
				if errors.As(err, &failure) {
					return httpx.Message(failure.status, false, failure.message), nil
				}
				return nil, err
			}
			if guard == nil {
				return httpx.JSON(struct {
					Success bool `json:"success"`
				}{true}), nil
			}
			return httpx.JSON(struct {
				Success bool          `json:"success"`
				Guard   *jsonx.Object `json:"publish_guard"`
			}{true, guard}), nil
		}))
	}
}

type customError struct {
	status  int
	message string
}

func (e *customError) Error() string { return e.message }
func reject(message string) error    { return &customError{400, message} }

func readCustom(ctx context.Context, id, engine string) (*jsonx.Object, error) {
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	settings := config.GetObject(engine)
	out := jsonx.Obj("config", settings.GetOr("custom_config", ""))
	switch engine {
	case "surge":
		out.Set("smart_groups", settings.GetOr("smart_groups", []any{}))
	case "mihomo":
		easytier, err := convert.NormalizeEasyTierSettings(settings.Value("easytier"))
		if err != nil {
			easytier = convert.EasyTierDefaultSettings()
		}
		out.Set("easytier", easytier)
		template, err := yamlbridge.Encode(convert.DefaultMihomoTemplate())
		if err != nil {
			return nil, err
		}
		raw, err := yamlx.Marshal(template)
		if err != nil {
			return nil, err
		}
		out.Set("default_config", string(raw))
		out.Set("provider_update_interval", settings.GetOr("provider_update_interval", jsonx.Int(3600)))
		reserved, hosts, err := agentContext(ctx, id)
		if err != nil {
			return nil, err
		}
		ports := make([]int, 0, len(reserved))
		for port := range reserved {
			ports = append(ports, port)
		}
		sort.Ints(ports)
		list := []any{}
		for _, port := range ports {
			list = append(list, jsonx.Obj("port", port, "owner", reserved[port]))
		}
		mode, err := convert.TakeoverMode(config)
		if err != nil {
			return nil, err
		}
		var takeover any
		if mode != "" {
			takeover = mode
		}
		out.Set("base_settings", jsonx.Obj("takeover", takeover, "reserved_ports", list, "local_addresses", hosts))
	}
	return out, nil
}

// saveCustom owns request detachment and the final draft commit. The OxiDNS
// branch updates freshly locked state, preserving concurrent editor fields.
func saveCustom(ctx context.Context, id, engine string, body customRequest) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if engine == "oxidns" {
		var text string
		if len(body.Config) == 0 || string(body.Config) == "null" || json.Unmarshal(body.Config, &text) != nil {
			return nil, reject(msgYAMLText)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err := repo.UpdateConfigTransaction(ctx, func(ctx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			data, err := json.Marshal(current.GetObject("oxidns"))
			if err != nil {
				return nil, err
			}
			node, err := yamlx.FromJSON(data)
			if err != nil {
				return nil, err
			}
			yamlx.Set(node, "custom_config", yamlx.Str(text))
			node, err = oxidns.NormalizeSettings(node, false)
			if err != nil {
				return nil, reject(err.Error())
			}
			data, err = yamlx.ToJSON(node)
			if err != nil {
				return nil, err
			}
			var stored jsonx.Object
			if err = json.Unmarshal(data, &stored); err != nil {
				return nil, err
			}
			current.Set("oxidns", &stored)
			return nil, nil
		}, id)
		return nil, err
	}
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	settings := config.GetObject(engine)
	if !config.Has(engine) {
		settings = jsonx.NewObject()
		config.Set(engine, settings)
	}
	if engine == "mihomo" {
		if len(body.Interval) > 0 {
			value, err := jsonx.Parse(body.Interval)
			if err != nil {
				return nil, err
			}
			interval, err := refresh.ValidateRefreshInterval(value, msgMihomoInterval)
			if err != nil {
				return nil, reject(err.Error())
			}
			settings.Set("provider_update_interval", jsonx.Int(interval))
		}
		var text string
		if len(body.Config) > 0 {
			if err = json.Unmarshal(body.Config, &text); err != nil && string(body.Config) != "null" {
				return nil, &customError{500, "configuration text expected"}
			}
		}
		if strings.TrimSpace(text) != "" {
			node, err := yamlx.Parse([]byte(text))
			if err != nil {
				return nil, reject(fmt.Sprintf(msgYAMLSyntax, err))
			}
			document, err := legacyYAML(node)
			if err != nil {
				return nil, reject(err.Error())
			}
			if err = convert.ValidateIPv6Flags(document); err != nil {
				return nil, reject(err.Error())
			}
			if node.Kind == yaml.MappingNode {
				previous, err := savedMihomoBase(settings)
				if err != nil {
					return nil, reject(err.Error())
				}
				reserved, hosts, err := agentContext(ctx, id)
				if err != nil {
					return nil, err
				}
				if err = convert.ValidateBaseSettings(document, previous, reserved, hosts); err != nil {
					return nil, reject(err.Error())
				}
			}
		}
		if len(body.EasyTier) > 0 {
			value, err := jsonx.Parse(body.EasyTier)
			if err != nil {
				return nil, err
			}
			normalized, err := convert.NormalizeEasyTierSettings(value)
			if err != nil {
				return nil, reject(fmt.Sprintf(msgEasyTier, err))
			}
			settings.Set("easytier", normalized)
		}
	}
	if len(body.Config) > 0 {
		value, err := jsonx.Parse(body.Config)
		if err != nil {
			return nil, err
		}
		settings.Set("custom_config", value)
	} else if engine == "mihomo" || engine == "mosdns" {
		settings.Set("custom_config", "")
	}
	if engine == "surge" && len(body.SmartGroups) > 0 {
		value, err := jsonx.Parse(body.SmartGroups)
		if err != nil {
			return nil, err
		}
		settings.Set("smart_groups", value)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if engine == "loon" {
		return nil, repo.SaveConfig(ctx, config, id)
	}
	return repo.SaveConfigWithPublishGuard(ctx, config, engine, id)
}

func legacyYAML(node *yamlx.Node) (any, error) {
	if err := yamlx.ProjectJSONKeys(node, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func savedMihomoBase(settings *jsonx.Object) (any, error) {
	text, ok := settings.Value("custom_config").(string)
	if !ok || strings.TrimSpace(text) == "" {
		return convert.DefaultMihomoTemplate(), nil
	}
	node, err := yamlx.Parse([]byte(text))
	if errors.Is(err, yamlx.ErrSyntax) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if node.Kind != yaml.MappingNode {
		return nil, nil
	}
	return legacyYAML(node)
}

type agentSource struct {
	ids     []any
	records map[string]*jsonx.Object
}

func (s agentSource) ProfileAgentIDs(string) ([]any, error) { return s.ids, nil }
func (s agentSource) AgentByID(id any) *jsonx.Object        { text, _ := id.(string); return s.records[text] }
func agentContext(ctx context.Context, id string) (map[int]string, []any, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	if id == "" {
		id, err = repo.ResolveProfileID(ctx, "")
		if err != nil {
			return nil, nil, err
		}
	}
	bindings, err := r.GetProfileAgentBindings(ctx, id)
	if err != nil {
		return map[int]string{}, []any{}, nil
	}
	system, err := r.GetSystem(ctx)
	if err != nil {
		return nil, nil, err
	}
	source := agentSource{records: map[string]*jsonx.Object{}}
	for _, value := range bindings.GetList("agents") {
		if agent, ok := jsonx.AsObject(value); ok {
			source.ids = append(source.ids, agent.Value("id"))
		}
	}
	for _, value := range system.GetList("agents") {
		if agent, ok := jsonx.AsObject(value); ok {
			source.records[agent.GetString("id")] = agent
		}
	}
	reserved, _, hosts := convert.AgentContext(source, id)
	return reserved, hosts, nil
}
