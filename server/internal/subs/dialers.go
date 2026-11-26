package subs

import (
	"regexp"
	"strings"

	"configflow/internal/logx"
)

var dialersLogger = logx.Get("backend.utils.subscription_dialers")

// Limits and settings of subscription dialer records.
const (
	MaxTargets  = 50
	MaxMembers  = 100
	MaxReported = 20
)

var dialerBuiltins = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "PASS-RULE": true, "COMPATIBLE": true}

var groupTypes = []string{"select", "url-test", "fallback", "load-balance"}

// groupKeys are the health-check and selection settings kept from the airport's group.
var groupKeys = []string{"url", "interval", "tolerance", "lazy", "timeout", "expected-status", "strategy", "max-failed-times"}

// GroupName is group_name: `<subscription>/<target>`.
func GroupName(subscription, target string) string { return subscription + "/" + target }

var portableFilterPunctuation = strings.NewReplacer("-", `\-`, "&", `\&`, "~", `\~`, "#", `\#`, " ", `\ `, "\t", "\\\t", "\n", "\\\n", "\r", "\\\r", "\v", "\\\v", "\f", "\\\f")

// quoteProviderLiteral preserves the emitted portable literal grammar. QuoteMeta
// handles regex metacharacters; whitespace and extended-mode punctuation retain
// the historical escaping required by generated provider filters.
func quoteProviderLiteral(text string) string {
	return portableFilterPunctuation.Replace(regexp.QuoteMeta(text))
}

func namePattern(name string) string {
	return strings.ReplaceAll(quoteProviderLiteral(name), "`", `\x60`)
}
