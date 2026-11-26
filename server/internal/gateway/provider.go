package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"configflow/internal/weburl"
)

// Provider naming and refresh.
const (
	// ProviderPrefix starts the name of every rule provider the gateway adds.
	ProviderPrefix = "cf_gateway_"
	// FakeProvider is the inline provider holding the fake-IP network.
	FakeProvider = ProviderPrefix + "fake_ip"
	// RefreshInterval is how often Mihomo refreshes the gateway providers
	// (seconds).
	RefreshInterval = 300
)

// ContentLoader returns the text of a selected rule set that has no inline
// content (library nil: not from the shared library). Publishing passes one
// that reads the publication's own snapshot.
type ContentLoader func(ctx context.Context, item, library *Rule) (string, error)

// ProviderName is the stable rule provider name of a selected rule set.
func ProviderName(ruleID string) string {
	sum := sha256.Sum256([]byte(ruleID))
	return ProviderPrefix + hex.EncodeToString(sum[:])[:20]
}

// ProviderPath is the panel path Mihomo refreshes a selected rule set from:
// its gateway IPv4 networks, or with source its original rule lines.
func (c *Config) ProviderPath(ruleID string, source bool) string {
	kind := "rules/"
	if source {
		kind = "source/"
	}
	suffix := "/linux-gateway/" + kind + weburl.Quote(ruleID)
	if c.profileID.truthy {
		return "/api/profiles/" + c.profileID.text + suffix
	}
	return "/api/custom-config" + suffix
}

// selectedContent is the behavior and text of a rule set that currently
// feeds the gateway; ok false when it does not (gateway off, not selected,
// gone or unusable).
func (c *Config) selectedContent(ctx context.Context, ruleID string, load ContentLoader) (behaviorName, content string, ok bool, err error) {
	settings, err := c.Settings()
	if err != nil {
		return "", "", false, err
	}
	item := c.RuleSet(ruleID)
	if !settings.Enabled || !slices.Contains(settings.RuleSetIDs, ruleID) || c.Reason(item) != "" {
		return "", "", false, nil
	}
	library := c.Library(item)
	src := source(item, library)
	switch {
	case src.sourceType.or("") == "content":
		if src.content.present && !src.content.isString {
			return "", "", false, gatewayError(msgContentTooLarge)
		}
		content = src.content.or("")
	case load != nil:
		if content, err = load(ctx, item, library); err != nil {
			return "", "", false, err
		}
	default:
		if content, err = c.storedContent(ctx, item, library, true); err != nil {
			return "", "", false, err
		}
	}
	return behavior(item, library), content, true, nil
}

// ProviderPayload is the IPv4 networks Mihomo takes over for a selected
// rule set (an empty list when the rule set does not feed the gateway).
// load nil reads the rule library (local cache, else a download); a remote
// set that cannot be read and has no cache file is an *Error, so Mihomo
// keeps its previous provider instead of clearing it.
func (c *Config) ProviderPayload(ctx context.Context, ruleID string, load ContentLoader) ([]string, error) {
	b, content, ok, err := c.selectedContent(ctx, ruleID, load)
	if err != nil || !ok {
		return []string{}, err
	}
	networks, err := ExtractCIDRs(content, b)
	if err != nil {
		return nil, err
	}
	return prefixStrings(networks), nil
}

// ProviderYAML is ProviderPayload as a Mihomo rule provider file
// ("payload:" list).
func (c *Config) ProviderYAML(ctx context.Context, ruleID string, load ContentLoader) (string, error) {
	payload, err := c.ProviderPayload(ctx, ruleID, load)
	if err != nil {
		return "", err
	}
	return dumpPayload(payload)
}

// SourceYAML is the selected rule set's original rules as a Mihomo rule
// provider file, refreshed by its stable ID too: comments dropped, IP rules
// normalized to networks ("8.8.8.8" → "8.8.8.8/32") exactly as the capture
// reads them, other classical rules kept as written. The content must pass
// ExtractCIDRs first, so a set the capture refuses is not accepted here
// either. An unselected or unusable rule set gives an empty payload.
func (c *Config) SourceYAML(ctx context.Context, ruleID string, load ContentLoader) (string, error) {
	b, content, ok, err := c.selectedContent(ctx, ruleID, load)
	if err != nil {
		return "", err
	}
	payload := []string{}
	if ok {
		if _, err := ExtractCIDRs(content, b); err != nil {
			return "", err
		}
		lines, err := contentLines(content)
		if err != nil {
			return "", err
		}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
				continue
			}
			kind, _, _ := strings.Cut(line, ",")
			kind = strings.ToUpper(kind)
			if b == "ipcidr" || kind == "IP-CIDR" || kind == "IP-CIDR6" {
				if line, err = normalizeIPRule(withoutComment(line), b); err != nil {
					return "", err
				}
			}
			if line != "" {
				payload = append(payload, line)
			}
		}
	}
	return dumpPayload(payload)
}

// normalizeIPRule writes the network of an IP rule as a prefix: Mihomo's IP
// strategy parses prefixes, not bare addresses.
func normalizeIPRule(line, b string) (string, error) {
	if b == "ipcidr" {
		network, ok := parseNetwork(line)
		if !ok {
			return "", gatewayError(msgInvalidCIDR)
		}
		return network.String(), nil
	}
	fields := splitFields(line)
	if len(fields) < 2 {
		return "", gatewayError(msgClassicalShape)
	}
	network, ok := parseNetwork(fields[1])
	if !ok {
		return "", gatewayError(msgInvalidCIDR)
	}
	fields[0] = strings.ToUpper(fields[0])
	fields[1] = network.String()
	return strings.Join(fields, ","), nil
}
