package oxidns

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// Profile is an owned DNS projection of one captured typed repository view.
// Rules, groups, Agents and system settings are read from the typed source;
// the OxiDNS settings object is decoded once from its stored JSON.
type Profile struct {
	source          *model.Config
	settings        *Node
	notes           json.RawMessage
	notesChanged    func(json.RawMessage)
	notesNull       bool
	notesNonText    bool
	presenceMu      sync.Mutex
	presenceVersion uint64
	presence        nativeIPResolver
	issues          []model.FieldIssue
}

// NewTypedProfile projects the consumed members without encoding
// unrelated settings, subscriptions and Agent deployment histories.
func NewTypedProfile(source *model.Config) (*Profile, error) {
	if source == nil || source.Profile == nil {
		source = &model.Config{Profile: &model.Profile{}}
	}
	profile := source.Profile
	settings, err := profile.OxiDNS.Node()
	if err != nil {
		return nil, err
	}
	var issues []model.FieldIssue
	settings, issues = storedProjection(settings)
	if isNone(settings) {
		settings = nil
	}
	notes, err := profile.DeviceNotes.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Stored notes Python refused to extend (null, a name that is not text).
	return &Profile{source: source, settings: settings, notes: notes, issues: issues,
		notesNull: profile.DeviceNotes.Null, notesNonText: profile.DeviceNotes.NonTextName}, nil
}

// ruleConfigs is the profile's rule list (disabled rules included).
func (p *Profile) ruleConfigs() []model.RuleConfig { return p.source.Profile.RuleConfigs }

// profileID is the profile's own id ("" for an unnamed projection).
func (p *Profile) profileID() string { return p.source.ProfileID }

// defaultProfileID is the system's default profile id.
func (p *Profile) defaultProfileID() string {
	if id := p.source.SystemConfig.DefaultProfileID; id != nil && *id != "" {
		return *id
	}
	return "default"
}

// agents returns the configuration's Agents.
func (p *Profile) agents() []model.Agent { return p.source.Agents }

// ProfileFromJSON decodes only DNS/profile metadata; unrelated fields do not
// enter model.Profile.Raw or trigger whole-configuration decoding.
func ProfileFromJSON(data []byte) (*Profile, error) {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var selected model.Fields
	for _, key := range []string{"oxidns", "mosdns", "rule_configs", "rules", "rule_sets", "proxy_groups", "device_notes"} {
		if raw, ok := fields.Get(key); ok {
			if err := selected.Set(key, raw); err != nil {
				return nil, err
			}
		}
	}
	data, err := selected.MarshalJSON()
	if err != nil {
		return nil, err
	}
	profile := new(model.Profile)
	if err = json.Unmarshal(data, profile); err != nil {
		return nil, err
	}
	source := &model.Config{Profile: profile}
	if raw, ok := fields.Get("profile_id"); ok {
		var projection model.Fields
		if err = projection.Set("profile_id", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var identity profileIdentity
		if err = json.Unmarshal(data, &identity); err != nil {
			return nil, err
		}
		source.ProfileID = identity.ID
	}
	if raw, ok := fields.Get("system_config"); ok {
		var projection model.Fields
		if err = projection.Set("system_config", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var system model.System
		if err = json.Unmarshal(data, &system); err != nil {
			return nil, err
		}
		source.SystemConfig = system.SystemConfig
	}
	if raw, ok := fields.Get("rule_library"); ok {
		var shared model.Shared
		var projection model.Fields
		if err = projection.Set("rule_library", raw); err != nil {
			return nil, err
		}
		bytes, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(bytes, &shared); err != nil {
			return nil, err
		}
		source.RuleLibrary = shared.RuleLibrary
	}
	if raw, ok := fields.Get("agents"); ok {
		var projection model.Fields
		if err := projection.Set("agents", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var system model.System
		if err := json.Unmarshal(data, &system); err != nil {
			return nil, err
		}
		source.Agents = system.Agents
		source.HasAgents = true
	}

	var resources model.Fields
	for _, key := range []string{"nodes", "subscriptions", "subscription_aggregations"} {
		if raw, ok := fields.Get(key); ok {
			if err = resources.Set(key, raw); err != nil {
				return nil, err
			}
		}
	}
	resourceData, err := resources.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var shared model.Shared
	if err = json.Unmarshal(resourceData, &shared); err != nil {
		return nil, err
	}
	source.Nodes = shared.Nodes
	source.Subscriptions = shared.Subscriptions
	source.Aggregations = shared.Aggregations
	return NewTypedProfile(source)
}

// Settings returns the owned settings tree; missing settings are nil.
func (p *Profile) Settings() *Node {
	if p == nil {
		return nil
	}
	return p.settings
}
func (p *Profile) setDeviceNotes(notes json.RawMessage) {
	p.notes = notes
	if p.notesChanged != nil {
		p.notesChanged(notes)
	}
}

var errUnsupportedYAML = fail("OxiDNS YAML 只支持普通对象、数组和标量")

func safeLoad(text string) (*Node, bool, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		if errors.Is(err, yamlx.ErrSyntax) {
			return nil, false, nil
		}
		return nil, false, yamlReaderFailure(err)
	}
	yamlx.MaterializeAliases(root)
	return root, true, nil
}
func (p *Profile) ruleProxyURL(baseURL, url, part string) (string, error) {
	credentials := mosdns.ProxyCredentials(p.source.SystemConfig)
	source := &model.Config{Profile: &model.Profile{}, ProfileID: p.profileID(), SystemConfig: model.SystemConfig{RuleProxyToken: credentials.Current}}
	return mosdns.RuleProxyURL(source, baseURL, url, part, "")
}
func rulesetParts(behavior string) []string { return mosdns.Parts(behavior) }
func convertRule(text, part string) (string, error) {
	return mosdns.ConvertRuleContentForMosDNS(text, part)
}
func parseUpstreams(text string) (*Node, error) {
	nodes, err := mosdns.Upstreams(text)
	if err != nil {
		return nil, err
	}
	return seqOf(nodes), nil
}
func parseCustomMatches(list *Node) (*Node, error) {
	var settings model.MosDNSSettings
	data := append([]byte(`{"custom_matches":`), ToJSON(list)...)
	data = append(data, '}')
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return seqOf(mosdns.MatchEntries(settings.CustomMatches)), nil
}
func (p *Profile) renderMembers(group model.ProxyGroup) *Node {
	return rules.RenderGroupDocument(group, rules.GroupResources(p.source))
}

// NativeClassicalIPLinesFunc queries offline content from typed metadata only.
type NativeClassicalIPLinesFunc func(context.Context, model.RuleConfig, []model.RuleLibraryItem) (*bool, error)

type nativeIPResolver func(context.Context, model.RuleConfig) (*bool, error)
type nativeIPFactory func([]model.RuleLibraryItem) nativeIPResolver

var classicalHook struct {
	sync.RWMutex
	factory nativeIPFactory
	version uint64
}

// SetNativeClassicalIPLines installs the typed offline metadata callback.
func SetNativeClassicalIPLines(fn NativeClassicalIPLinesFunc) func() {
	if fn == nil {
		return setIPFactory(nil)
	}
	return setIPFactory(func(library []model.RuleLibraryItem) nativeIPResolver {
		return func(ctx context.Context, item model.RuleConfig) (*bool, error) { return fn(ctx, item, library) }
	})
}
func setIPFactory(factory nativeIPFactory) func() {
	classicalHook.Lock()
	previous := classicalHook.factory
	classicalHook.factory = factory
	classicalHook.version++
	classicalHook.Unlock()
	return func() {
		classicalHook.Lock()
		classicalHook.factory = previous
		classicalHook.version++
		classicalHook.Unlock()
	}
}
func (p *Profile) classicalWithoutIP(item model.RuleConfig) bool {
	classicalHook.RLock()
	factory, version := classicalHook.factory, classicalHook.version
	classicalHook.RUnlock()
	if factory == nil {
		return false
	}
	p.presenceMu.Lock()
	if p.presence == nil || p.presenceVersion != version {
		p.presence = factory(p.source.RuleLibrary)
		p.presenceVersion = version
	}
	resolver := p.presence
	p.presenceMu.Unlock()
	rule := model.RuleConfig{ID: item.ID, Name: item.Name, LibraryRuleID: item.LibraryRuleID,
		URL: item.URL, Format: item.Format, SourceType: item.SourceType, Content: item.Content, Behavior: item.Behavior}
	// Generate owns only a snapshot, not a write-capable Runtime context. Wire
	// installs the peek-only reader so this offline probe cannot migrate files.
	value, err := resolver(context.Background(), rule)
	return err == nil && value != nil && !*value
}

func emptyProfile() *Profile {
	return &Profile{source: &model.Config{Profile: &model.Profile{}}}
}

type profileIdentity struct {
	ID string `json:"profile_id"`
}

type profileIdentityWire profileIdentity

func (v *profileIdentity) UnmarshalJSON(data []byte) error {
	return model.DecodeLenient(data, (*profileIdentityWire)(v))
}
