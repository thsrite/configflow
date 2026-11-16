// Package discovery aggregates observed domains, manages discovery rules and
// runs bounded probe, website and region jobs. It never collects device traffic.
package discovery

import (
	"configflow/internal/subs"
	"context"
	"crypto/md5"
	"encoding/hex"
)

// RegistrableDomain extracts the public registrable domain without network I/O.
func RegistrableDomain(host string) string {
	_, domain, suffix := subs.ExtractDomain(host, false)
	if domain != "" && suffix != "" {
		return domain + "." + suffix
	}
	return host
}

// providerRevisions hashes the bytes served by active discovery rule providers.
func providerRevisions(ctx context.Context, profileID string) (map[string]string, error) {
	c, err := loadConfig(ctx, profileID)
	if err != nil {
		return nil, err
	}
	ids := []*string{c.Settings.DirectRuleset, c.Settings.ProxyRuleset}
	for _, id := range c.Settings.RegionalRulesets {
		ids = append(ids, ptr(id))
	}
	out := map[string]string{}
	for _, id := range ids {
		v := info(c, id)
		if v == nil || v.Problem != nil || !v.Active {
			continue
		}
		b, err := cachedContent(ctx, *c.library(*id))
		if err != nil {
			return nil, err
		}
		sum := md5.Sum(b)
		out[*v.Name] = hex.EncodeToString(sum[:])
	}
	return out, nil
}
