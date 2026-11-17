package deploy

import (
	"bytes"
	"configflow/internal/agents/diag"
	"configflow/internal/gateway"
	"configflow/internal/logx"
	"configflow/internal/rulefetch"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

var logger = logx.Get("backend.agents.deployment_bundle")

// RuleRef is one captured rule declaration; SourceType distinguishes inline content.
type RuleRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	SourceType string `json:"source_type"`
	Content    string `json:"content"`
	Behavior   string `json:"behavior"`
	Format     string `json:"format"`
}

// MaterializeOptions controls recursion, conversion and per-build raw sharing.
type MaterializeOptions struct {
	Depth         int
	RawMosDNS     bool
	FetchRaw      func(context.Context, string) ([]byte, error)
	SourceID      string
	DeclaredRule  *RuleRef
	RemoteSources map[string]bool
}

// LocalRule resolves recognized local callbacks only against the snapshot.
func LocalRule(rawURL string, src *Source, baseURL string, rulesLocalOnly bool) (*RuleRef, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if (parsed.Scheme != "" || parsed.Host != "") && !src.internal(rawURL, baseURL) {
		return nil, nil
	}
	prefix := sourcePrefix(src, baseURL)
	p := parsed.Path
	for _, rule := range src.library() {
		local := src.profilePath("/rules/local/" + rule.Name)
		library := src.profilePath("/rule-library/content/" + rule.ID)
		if p == local || p == prefix+local {
			return &rule, nil
		}
		if p == library || p == prefix+library {
			if rulesLocalOnly {
				return nil, nil
			}
			return &rule, nil
		}
	}
	if strings.Contains(p, "/api/") || strings.HasPrefix(p, "/rules/local/") || strings.HasPrefix(p, "/rule-library/content/") {
		return nil, preparation("Rule callback is not part of this profile snapshot")
	}
	return nil, nil
}
func sourcePrefix(src *Source, baseURL string) string {
	u, _ := url.Parse(src.serverDomain(baseURL))
	if u == nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

// MaterializeRule resolves callbacks without a second HTTP read of panel state.
func MaterializeRule(ctx context.Context, rawURL string, src *Source, baseURL string, o MaterializeOptions) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.Depth > 3 {
		return nil, preparation("Recursive rule callback")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	internal := (u.Scheme == "" && u.Host == "") || src.internal(rawURL, baseURL)
	if internal && (strings.HasSuffix(u.Path, "/mosdns/rule-proxy") || strings.HasSuffix(u.Path, "/oxidns/rule-proxy")) {
		original := u.Query().Get("url")
		if original == "" {
			return nil, preparation("Rule conversion callback has no source")
		}
		next := o
		next.Depth++
		next.SourceID = u.Query().Get("source_id")
		data, err := MaterializeRule(ctx, original, src, baseURL, next)
		if err != nil || o.RawMosDNS {
			return data, err
		}
		return ConvertMosDNSRules(data, "domain_set", false)
	}
	if internal && src != nil {
		gw := src.gateway()
		settings, err := gw.Settings()
		if err != nil {
			return nil, err
		}
		prefix := sourcePrefix(src, baseURL)
		for _, id := range settings.RuleSetIDs {
			for _, source := range []bool{false, true} {
				expected := gw.ProviderPath(id, source)
				expected, _ = url.PathUnescape(expected)
				if u.Path != expected && u.Path != prefix+expected {
					continue
				}
				loader := func(ctx context.Context, item, library *gateway.Rule) (string, error) {
					rule := library
					if rule == nil {
						rule = item
					}
					next := o
					next.Depth++
					next.RawMosDNS = false
					next.SourceID = ""
					next.DeclaredRule = nil
					content, err := MaterializeRule(ctx, rule.URL(), src, baseURL, next)
					if err != nil {
						return "", err
					}
					content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
					if !utf8.Valid(content) {
						return "", preparation("Gateway rules must be UTF-8 text")
					}
					return string(content), nil
				}
				var rendered string
				if source {
					rendered, err = gw.SourceYAML(ctx, id, loader)
				} else {
					rendered, err = gw.ProviderYAML(ctx, id, loader)
				}
				var gwErr *gateway.Error
				if errors.As(err, &gwErr) {
					return nil, &PreparationError{Message: err.Error(), cause: err}
				}
				return []byte(rendered), err
			}
		}
	}
	rule, err := LocalRule(rawURL, src, baseURL, false)
	if err != nil {
		return nil, err
	}
	if rule != nil {
		if rule.SourceType == "content" {
			return []byte(rule.Content), nil
		}
		if rule.URL == rawURL {
			return nil, preparation("Rule callback refers to itself")
		}
		next := o
		next.Depth++
		next.SourceID = rule.ID
		return MaterializeRule(ctx, rule.URL, src, baseURL, next)
	}
	if o.RemoteSources != nil {
		o.RemoteSources[rawURL] = true
	}
	fetch := o.FetchRaw
	if fetch == nil {
		fetch = func(ctx context.Context, u string) ([]byte, error) { return fetchBytes(ctx, u, src) }
	}
	data, stale, err, declared := src.readRule(ctx, rawURL, o.SourceID, o.DeclaredRule, fetch)
	if declared {
		if err != nil {
			var validation *rulefetch.RemoteRuleValidationError
			if errors.As(err, &validation) {
				return nil, &PreparationError{Message: err.Error(), cause: err}
			}
			return nil, err
		}
		if stale != nil {
			logger.Warningf(msgRuleStale, diag.FailureText(displayName(o.SourceID), nil, 120), diag.FailureText(stale.Error(), nil, 0))
		}
		return data, nil
	}
	return fetch(ctx, rawURL)
}

func fetchBytes(ctx context.Context, rawURL string, src *Source) ([]byte, error) {
	if rawURL == "" {
		return nil, preparation("Required rule has no source URL")
	}
	// requestRule retains Python's per-attempt (10 s connect, 60 s read)
	// timeouts. The caller job context controls shutdown; do not shorten
	// multi-candidate/retried downloads with an extra overall deadline.
	response, err := src.requestRule(ctx, rawURL)
	if err == nil {
		defer response.Close()
		err = response.RaiseForStatus()
		if err == nil {
			var value []byte
			value, err = io.ReadAll(io.LimitReader(response, MaxFileBytes+1))
			if err == nil {
				if len(value) > MaxFileBytes {
					return nil, preparation("Rule exceeds deployment file size limit")
				}
				return value, nil
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	message, remedy := downloadFailure(rawURL, src, err)
	return nil, &RuleDownloadError{&PreparationError{Message: message + "\n" + fmt.Sprintf(msgRawError, diag.FailureText(fmt.Sprintf("%T: %v", err, err), nil, 0)), Remedy: remedy, cause: err}}
}

func downloadFailure(rawURL string, src *Source, err error) (string, *Remedy) {
	var multiple *rulefetch.RuleFetchError
	prefix := ""
	if errors.As(err, &multiple) {
		prefix = msgDownloadMirrors
		err = multiple.LastError
	}
	var fetch *rulefetch.FetchError
	message := msgDownloadFailed
	var remedy *Remedy
	if errors.As(err, &fetch) {
		switch fetch.Class {
		case "Timeout", "ConnectTimeout", "ReadTimeout":
			message = msgDownloadTimeout
		case "ProxyError":
			message = msgDownloadProxy
		case "SSLError":
			message = msgDownloadTLS
		case "ConnectionError":
			message = msgDownloadConnect
		case "HTTPError":
			message = fmt.Sprintf(msgDownloadHTTP, fetch.Status)
		}
		if fetch.Class != "HTTPError" && !src.internal(rawURL, "") {
			remedy = &Remedy{Kind: "rule_download", Github: rulefetch.IsGithubURL(src.githubSource(rawURL))}
		}
	}
	return prefix + message, remedy
}
