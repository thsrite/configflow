package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
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
	*r = customRequest{Config: fields["config"], SmartGroups: fields["smart_groups"], Interval: fields["provider_update_interval"], EasyTier: fields["easytier"]}
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
			data, err := c.Data()
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				if engine == "oxidns" {
					return httpx.Message(400, false, msgYAMLText), nil
				}
				return httpx.Message(500, false, msgObject), nil
			} else {
				var raw json.RawMessage
				if engine == "oxidns" {
					if !c.BindSilent(&raw) {
						return httpx.Message(400, false, msgYAMLText), nil
					}
				} else {
					if err := c.Bind(&raw); err != nil {
						return nil, err
					}
				}
				if string(raw) == "null" {
					switch engine {
					case "oxidns":
						return httpx.Message(400, false, msgYAMLText), nil
					case "mihomo", "mosdns":
						return httpx.Message(500, false, msgObject), nil
					}
				} else {
					if len(raw) == 0 || raw[0] != '{' {
						return httpx.Message(400, false, msgObject), nil
					}
					if err := json.Unmarshal(raw, &body); err != nil {
						return nil, err
					}
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
				Success bool            `json:"success"`
				Guard   json.RawMessage `json:"publish_guard"`
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

func readCustom(ctx context.Context, id, engine string) (map[string]any, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	config, err := r.Config(ctx, id, repo.ResourceRoots{})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(config.Profile)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	// Profiles may omit an engine section (loon is not required on import);
	// the handlers' setdefault reads such a profile as empty settings.
	var settings map[string]json.RawMessage
	if section, present := fields[engine]; present {
		if err = json.Unmarshal(section, &settings); err != nil {
			return nil, err
		}
	}
	out := map[string]any{"config": ""}
	if raw, ok := settings["custom_config"]; ok {
		out["config"] = raw
	}
	switch engine {
	case "surge":
		out["smart_groups"] = json.RawMessage(`[]`)
		if raw, ok := settings["smart_groups"]; ok {
			out["smart_groups"] = raw
		}
	case "mihomo":
		easytier, err := convert.NormalizeEasyTier(config.Profile.Mihomo.EasyTier)
		if err != nil {
			easytier = convert.DefaultEasyTier()
		}
		out["easytier"] = easytier
		raw, err := yamlx.Marshal(convert.MihomoTemplate())
		if err != nil {
			return nil, err
		}
		out["default_config"] = string(raw)
		out["provider_update_interval"] = 3600
		if raw, ok := settings["provider_update_interval"]; ok {
			out["provider_update_interval"] = raw
		}
		reserved, hosts, err := agentContext(ctx, id)
		if err != nil {
			return nil, err
		}
		ports := make([]int, 0, len(reserved))
		for port := range reserved {
			ports = append(ports, port)
		}
		sort.Ints(ports)
		list := []map[string]any{}
		for _, port := range ports {
			list = append(list, map[string]any{"port": port, "owner": reserved[port]})
		}
		mode, err := convert.GatewayMode(config)
		if err != nil {
			return nil, err
		}
		var takeover any
		if mode != "" {
			takeover = mode
		}
		out["base_settings"] = map[string]any{"takeover": takeover, "reserved_ports": list, "local_addresses": hosts}
	}
	return out, nil
}

func saveCustom(ctx context.Context, id, engine string, body customRequest) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, err
	}
	if engine == "oxidns" {
		var text string
		if len(body.Config) == 0 || string(body.Config) == "null" || json.Unmarshal(body.Config, &text) != nil {
			return nil, reject(msgYAMLText)
		}
		_, err = r.UpdateProfile(ctx, id, func(ctx context.Context, p *model.Profile) error {
			node, err := p.OxiDNS.Node()
			if err != nil {
				return err
			}
			yamlx.Set(node, "custom_config", yamlx.Str(text))
			node, err = oxidns.NormalizeSettings(node, false)
			if err != nil {
				return reject(err.Error())
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			return p.OxiDNS.SetNode(node)
		})
		return nil, err
	}
	config, err := r.Config(ctx, id, repo.ResourceRoots{})
	if err != nil {
		return nil, err
	}
	baseline, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	if err = fields.UnmarshalJSON(baseline); err != nil {
		return nil, err
	}
	raw, _ := fields.Get(engine)
	var settings model.Fields
	if len(raw) > 0 {
		if err = settings.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
	}
	if engine == "mihomo" {
		if len(body.Interval) > 0 {
			interval, err := refresh.ParseInterval(body.Interval, msgMihomoInterval)
			if err != nil {
				return nil, reject(err.Error())
			}
			raw, _ := json.Marshal(interval)
			if err = settings.Set("provider_update_interval", raw); err != nil {
				return nil, err
			}
		}
		var text string
		if len(body.Config) > 0 && json.Unmarshal(body.Config, &text) != nil {
			node, e := yamlx.FromJSON(body.Config)
			if e != nil || !emptyDraft(node) {
				return nil, &customError{500, "configuration text expected"}
			}
		}
		if strings.TrimSpace(text) != "" {
			node, err := yamlx.Parse([]byte(text))
			if err != nil {
				if errors.Is(err, yamlx.ErrSyntax) {
					return nil, reject(fmt.Sprintf(msgYAMLSyntax, err))
				}
				return nil, reject(err.Error())
			}
			if err = yamlx.ProjectJSONKeys(node, yamlx.StringifyScalarKeys); err != nil {
				return nil, reject(err.Error())
			}
			if err = convert.ValidateIPv6Node(node); err != nil {
				return nil, reject(err.Error())
			}
			if node.Kind == yaml.MappingNode {
				previousText := ""
				if original, present := settings.Get("custom_config"); present {
					_ = json.Unmarshal(original, &previousText)
				}
				previous, err := savedMihomoBase(previousText)
				if err != nil {
					return nil, reject(err.Error())
				}
				reserved, hosts, err := agentContext(ctx, id)
				if err != nil {
					return nil, err
				}
				addresses := []netip.Addr{}
				for _, host := range hosts {
					if ip, err := netip.ParseAddr(host); err == nil {
						addresses = append(addresses, ip)
					}
				}
				if err = convert.ValidateBaseNode(node, previous, reserved, addresses); err != nil {
					return nil, reject(err.Error())
				}
			}
		}
		if len(body.EasyTier) > 0 {
			var input *model.EasyTierSettings
			if err = json.Unmarshal(body.EasyTier, &input); err != nil {
				return nil, reject(fmt.Sprintf(msgEasyTier, "settings object expected"))
			}
			normalized, err := convert.NormalizeEasyTier(input)
			if err != nil {
				return nil, reject(fmt.Sprintf(msgEasyTier, err))
			}
			raw, err := json.Marshal(normalized)
			if err != nil {
				return nil, err
			}
			if err = settings.Set("easytier", raw); err != nil {
				return nil, err
			}
		}
	}
	if len(body.Config) > 0 {
		err = settings.Set("custom_config", body.Config)
	} else if engine == "mihomo" || engine == "mosdns" {
		err = settings.Set("custom_config", json.RawMessage(`""`))
	}
	if err != nil {
		return nil, err
	}
	if engine == "surge" && len(body.SmartGroups) > 0 {
		if err = settings.Set("smart_groups", body.SmartGroups); err != nil {
			return nil, err
		}
	}
	raw, err = settings.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if err = fields.Set(engine, raw); err != nil {
		return nil, err
	}
	raw, err = fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	_, revision, err := r.SaveProfileFieldsJSON(ctx, id, raw, baseline)
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	if engine == "loon" {
		return nil, nil
	}
	return json.Marshal(struct {
		Profile  string `json:"expected_profile_id"`
		Service  string `json:"expected_service_type"`
		Revision int64  `json:"expected_config_revision"`
	}{id, engine, revision})
}
func savedMihomoBase(text string) (*yamlx.Node, error) {
	if strings.TrimSpace(text) == "" {
		return convert.MihomoTemplate(), nil
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
	if err = yamlx.ProjectJSONKeys(node, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	return node, nil
}
func agentContext(ctx context.Context, id string) (map[int]string, []string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := r.ProfileAgentBindingsRecord(ctx, id)
	if err != nil {
		return map[int]string{}, []string{}, nil
	}
	system, err := r.System(ctx)
	if err != nil {
		return nil, nil, err
	}
	records := []model.Agent{}
	for _, binding := range bindings.Agents {
		var id string
		if json.Unmarshal(binding.ID, &id) != nil {
			continue
		}
		for _, a := range system.Agents {
			var agentID string
			if rawID, _, err := a.MemberJSON("id"); err == nil && json.Unmarshal(rawID, &agentID) == nil && agentID == id {
				records = append(records, a)
			}
		}
	}
	reserved, _, hosts := convert.AgentPorts(records)
	return reserved, hosts, nil
}

// emptyDraft is the custom-config form's accepted empty JSON alternative.
// The original value is still saved verbatim; only YAML validation is skipped.
func emptyDraft(n *yamlx.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) == 0
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return true
		case "!!bool":
			return n.Value == "false"
		case "!!str":
			return n.Value == ""
		case "!!int", "!!float":
			number, err := strconv.ParseFloat(n.Value, 64)
			return err == nil && number == 0
		}
	}
	return false
}
