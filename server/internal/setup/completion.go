package setup

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
	routers "configflow/internal/routers/spec"
	"configflow/internal/routersplit"
)

const routeRuleBudget = 30 * time.Second

type checkedInstallation struct {
	ID            string            `json:"id"`
	Fingerprint   string            `json:"fingerprint"`
	DigestVersion *model.Int        `json:"digest_version"`
	Mihomo        installedIdentity `json:"mihomo"`
	OxiDNS        installedIdentity `json:"oxidns"`
}
type installedIdentity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}
type routeInstruction struct {
	Gateway    string             `json:"gateway"`
	DNS        string             `json:"dns"`
	Routes     []instructionRoute `json:"routes"`
	Warnings   []string           `json:"warnings"`
	Incomplete bool               `json:"incomplete"`
	Missing    []string           `json:"missing_rule_sets,omitempty"`
}
type instructionRoute struct {
	routers.ProxyCIDR
	RequiresSourceExclusion bool `json:"requires_source_exclusion"`
}

func checkFullTUN(status GatewayStatus) error {
	if status.IPv4Forwarding == nil || status.TUN == nil {
		return failure(msgOldAgent, 409)
	}
	if status.IPv4Forwarding.Current != "1" {
		return failure(msgForwardingOff, 409)
	}
	if !enabled(status.IPv4Forwarding.Persisted) {
		return failure(msgForwardingNotPersisted, 409)
	}
	if status.Managed.Active || status.Managed.Pending || status.Managed.Error != "" {
		return failure(msgGatewayRemaining, 409)
	}
	if status.EnabledInConfig || status.TUN.GatewayMarker {
		return failure(msgPublishedGateway, 409)
	}
	if !enabled(status.TUN.Configured) {
		return failure(msgTUNNotConfigured, 409)
	}
	if !enabled(status.TUN.Present) {
		device := status.TUN.Device
		if device == "" {
			device = "Meta"
		}
		return failure(fmt.Sprintf(msgTUNNotRunning, device), 409)
	}
	return nil
}
func (s *Service) checkInstallation(ctx context.Context, record *Record) (json.RawMessage, error) {
	owner := s.owners.Installation
	if owner == nil || owner.Latest == nil || owner.GatewayStatus == nil {
		return nil, unavailable()
	}
	installation, err := owner.Latest(ctx, s.repository, record.ProfileID)
	if err != nil {
		return nil, err
	}
	config, revision, err := s.repository.PublishConfig(ctx, record.ProfileID, false)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	fingerprint, err := CoreFingerprint(raw)
	if err != nil {
		return nil, err
	}
	var checked checkedInstallation
	if len(record.Installation) > 0 {
		if err := jsondecode.Unmarshal(record.Installation, &checked); err != nil {
			return nil, ErrCorruptRecord
		}
	}
	if installation == nil || installation.Phase != "published" || installation.TargetChanged || installation.ConfigChanged && (checked.DigestVersion == nil || *checked.DigestVersion != digestVersion || checked.ID != installation.ID || checked.Fingerprint != fingerprint) {
		return nil, failure(msgNeedPublish, 409)
	}
	pair := installation.Agents
	if pair.Mihomo.ID == "" || pair.OxiDNS.ID == "" {
		return nil, failure(msgNeedPair, 409)
	}
	for _, agent := range []InstalledAgent{pair.Mihomo, pair.OxiDNS} {
		if agent.Enabled != nil && !*agent.Enabled || agent.Status != "online" {
			return nil, failure(msgAgentOffline, 409)
		}
	}
	fullTUN := record.Goal == "router" && fullTunProfile(config.Profile) && !enabled(record.LegacyTakeover)
	if fullTUN && s.owners.ReadRouteContent == nil {
		return nil, unavailable()
	}
	status, err := owner.GatewayStatus(ctx, s.repository, pair.Mihomo.ID, record.ProfileID)
	if err != nil {
		return nil, err
	}
	if fullTUN {
		if err := checkFullTUN(status); err != nil {
			return nil, err
		}
	} else if !status.Managed.Active || status.Managed.Pending || !enabled(status.Managed.Healthy) {
		return nil, failure(msgGatewayUnhealthy, 409)
	}
	checked = checkedInstallation{installation.ID, fingerprint, pointer(digestVersion), installedIdentity{pair.Mihomo.ID, pair.Mihomo.Name, pair.Mihomo.Host}, installedIdentity{pair.OxiDNS.ID, pair.OxiDNS.Name, pair.OxiDNS.Host}}
	record.Installation, err = json.Marshal(checked)
	if err != nil {
		return nil, err
	}
	ruleIDs := []string{}
	missing := []string{}
	var loader routers.ContentLoader
	if fullTUN {
		_, marker := routersplit.ReadMarkerProfile(config.Profile)
		ruleIDs = append(ruleIDs, marker.RouteRuleSetIDs...)
		loader = s.routeLoader(ctx, raw, &missing)
	} else {
		settings, err := gatewayRuleIDs(config)
		if err != nil {
			return nil, err
		}
		ruleIDs = settings
	}
	desired, err := routers.ComputeDesired(ctx, raw, pair.Mihomo.Host, ruleIDs, loader)
	if err != nil {
		return nil, err
	}
	instructions := routeInstruction{Gateway: pair.Mihomo.Host, DNS: pair.OxiDNS.Host, Routes: []instructionRoute{}, Warnings: desired.Warnings, Incomplete: len(missing) > 0, Missing: missing}
	for _, route := range desired.ProxyCIDRs {
		instructions.Routes = append(instructions.Routes, instructionRoute{route, route.CIDR != desired.FakeIPRange})
	}
	record.RouteInstructions, err = json.Marshal(instructions)
	if err != nil {
		return nil, err
	}
	record.ConfigRevision = pointer(model.Int(revision))
	return raw, nil
}

func (s *Service) routeLoader(ctx context.Context, config json.RawMessage, missing *[]string) routers.ContentLoader {
	clock := lifecycle.ClockFrom(ctx)
	budget := s.routeBudget
	if budget <= 0 {
		budget = routeRuleBudget
	}
	deadline := clock.Now().Add(budget)
	spawner := s.spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	return func(item routers.RuleSet, library *routers.LibraryRule) (content string, err error) {
		defer func() {
			if content == "" {
				name := item.Name
				if name == "" {
					name = item.ID
				}
				if name == "" {
					name = msgRuleSet
				}
				*missing = append(*missing, name)
			}
		}()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return s.owners.ReadRouteContent(ctx, config, item, library, RouteReadOptions{AllowNetwork: false})
		}
		options := RouteReadOptions{AllowNetwork: true, ConnectTimeout: min(5*time.Second, remaining), ReadTimeout: min(20*time.Second, remaining)}
		result := make(chan string, 1)
		// The tracked worker may fill the cache after the UI budget. It retains the
		// originating generation even if the default Runtime restarts meanwhile.
		origin := lifecycle.Detach(ctx)
		spawnErr := spawner.Spawn(origin, "setup-route-rule", func(job context.Context) {
			work, cancel := context.WithCancel(repo.WithScope(origin))
			defer cancel()
			defer repo.ResetConfigContext(work)
			stop := context.AfterFunc(job, cancel)
			defer stop()
			bounded, timeoutCancel := context.WithTimeout(work, remaining)
			defer timeoutCancel()
			text, _ := s.owners.ReadRouteContent(bounded, config, item, library, options)
			result <- text
		})
		if spawnErr != nil {
			return "", spawnErr
		}
		timer := clock.NewTimer(remaining)
		defer timer.Stop()
		select {
		case content = <-result:
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return content, nil
		case <-timer.C():
			return "", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (s *Service) finish(ctx context.Context, record *Record, body map[string]json.RawMessage) error {
	makeDefault, valid := readBool(body, "make_default", false)
	if !valid {
		return failure(msgMakeDefault, 400)
	}
	var shown routeInstruction
	if len(record.RouteInstructions) > 0 && jsondecode.Unmarshal(record.RouteInstructions, &shown) != nil {
		return ErrCorruptRecord
	}
	config, err := s.checkInstallation(ctx, record)
	if err != nil {
		return err
	}
	if record.Goal == "router" {
		fallback := "manual"
		if record.RouterMode != nil {
			fallback = *record.RouterMode
		}
		mode, valid := readString(body, "router_mode", fallback)
		if !valid {
			return failure(msgFinishRouterMode, 400)
		}
		switch mode {
		case "manual":
			var current routeInstruction
			if jsondecode.Unmarshal(record.RouteInstructions, &current) != nil {
				return ErrCorruptRecord
			}
			if len(current.Missing) > 0 {
				return failure(fmt.Sprintf(msgMissingRoutes, strings.Join(current.Missing, "」「")), 409)
			}
			if len(shown.Missing) > 0 {
				return failure(fmt.Sprintf(msgNewRoutes, strings.Join(shown.Missing, "」「")), 409)
			}
			routes, _ := readBool(body, "ack_routes", false)
			dns, _ := readBool(body, "ack_dns", false)
			if !routes || !dns {
				return failure(msgAcknowledgeRoutes, 409)
			}
			record.RouterMode, record.RouterVerification = pointer("manual"), pointer("manual")
		case "managed":
			id, valid := readString(body, "router_id", text(record.RouterID))
			if !valid {
				return failure(msgRouterPair, 409)
			}
			router, err := findRouter(config, id)
			if err != nil {
				return err
			}
			var pair checkedInstallation
			if jsondecode.Unmarshal(record.Installation, &pair) != nil {
				return ErrCorruptRecord
			}
			if router == nil || router.ProxyGatewayAgentID != pair.Mihomo.ID || router.DNSAgentID != pair.OxiDNS.ID {
				return failure(msgRouterPair, 409)
			}
			if s.owners.VerifyRouter == nil {
				return unavailable()
			}
			verification, err := s.owners.VerifyRouter(ctx, s.repository, record.ProfileID, config, router)
			if err != nil {
				return err
			}
			if len(verification.Blockers) > 0 || !verification.Empty {
				return failure(msgRouterUnsynced, 409)
			}
			latest, err := s.repository.Profile(ctx, record.ProfileID)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(latest)
			if err != nil {
				return err
			}
			current, err := findRouter(raw, id)
			if err != nil {
				return err
			}
			before, _ := json.Marshal(router)
			after, _ := json.Marshal(current)
			if !equalJSONValue(before, after) {
				return failure(msgRouterChanged, 409)
			}
			record.RouterMode, record.RouterID, record.RouterVerification = pointer("managed"), &id, pointer("verified")
		default:
			return failure(msgFinishRouterMode, 400)
		}
	} else {
		record.RouterVerification = pointer("client_pending")
	}
	latest, _, err := s.repository.PublishConfig(ctx, record.ProfileID, false)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(latest)
	if err != nil {
		return err
	}
	fingerprint, err := CoreFingerprint(raw)
	if err != nil {
		return err
	}
	var checked checkedInstallation
	if jsondecode.Unmarshal(record.Installation, &checked) != nil {
		return ErrCorruptRecord
	}
	if checked.DigestVersion == nil || *checked.DigestVersion != digestVersion || checked.Fingerprint != fingerprint {
		return failure(msgCoreChanged, 409)
	}
	record.Completed, record.Step, record.MaxStep = pointer(true), 5, 5
	previous, err := s.repository.DefaultProfileID(ctx)
	if err != nil {
		return err
	}
	if makeDefault && previous != record.ProfileID {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err = s.repository.SetDefaultProfile(ctx, record.ProfileID); err != nil {
			return err
		}
		record.PreviousDefault = &previous
	}
	return nil
}

func findRouter(config json.RawMessage, id string) (*routers.Router, error) {
	var profile struct {
		Routers struct {
			Items []routers.Router `json:"items"`
		} `json:"routers"`
	}
	if err := jsondecode.Unmarshal(config, &profile); err != nil {
		return nil, err
	}
	for i := range profile.Routers.Items {
		if profile.Routers.Items[i].ID == id {
			return &profile.Routers.Items[i], nil
		}
	}
	return nil, nil
}

// Keep spawner's contract checked at the native seam.
var _ lockx.Spawner = lifecycle.DefaultSpawner{}

func gatewayRuleIDs(config *model.Config) ([]string, error) {
	settings, err := gateway.SettingsOf(config)
	return settings.RuleSetIDs, err
}

func fullTunProfile(profile *model.Profile) bool {
	state, _ := routersplit.ReadMarkerProfile(profile)
	return state == routersplit.StateValid
}
