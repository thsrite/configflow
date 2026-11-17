package rules

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
)

type delivery struct {
	Content []byte
	Stale   bool
}
type sourceFailure struct {
	Status        int             `json:"-"`
	Success       bool            `json:"success"`
	Message       string          `json:"message"`
	Errors        json.RawMessage `json:"errors,omitempty"`
	CacheRetained *bool           `json:"cache_retained,omitempty"`
}

func (e *sourceFailure) Error() string { return e.Message }

// Local serves the public provider through the shared cache owner. Delivery
// normalization never changes source bytes or their validation metadata.
func (s *Service) Local(ctx context.Context, id, name, target, host string) (delivery, error) {
	return s.local(lifecycle.Detach(ctx), id, name, target, host)
}
func (s *Service) elapsed(start time.Time) float64 {
	return math.Round(s.now().Sub(start).Seconds()*100000) / 100
}

// Match tests a domain or IP lazily, fetching only rule sources actually reached.
func (s *Service) Match(ctx context.Context, id string, body record, host string) (record, error) {
	ctx = lifecycle.Detach(ctx)
	start := s.now()
	if body == nil {
		return nil, &fault{500, msgMatchFailed}
	}
	query, ok := textValue(body["query"])
	if _, exists := body["query"]; exists && !ok {
		return nil, &fault{500, msgMatchFailed}
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, invalid(msgQuery)
	}
	query = shared.NormalizeMatchQuery(query)
	if !shared.IsValidDomain(query, true) && !shared.IsValidIP(query) {
		return nil, invalid(msgQueryInvalid)
	}
	out, err := s.match(ctx, id, query, host)
	if err != nil {
		return nil, err
	}
	matched := out != nil
	if out == nil {
		out = record{"message": stringJSON(msgUnmatched)}
	}
	out["success"] = boolJSON(true)
	out["matched"] = boolJSON(matched)
	_ = out.set("elapsed_time", s.elapsed(start))
	return out, nil
}

type scanRow struct {
	ID, Name, Kind, Type, Value string
	Policy                      json.RawMessage
	Enabled                     bool
}
type occurrence struct {
	SourceType string          `json:"source_type"`
	Source     string          `json:"source"`
	RuleID     string          `json:"rule_id"`
	Policy     json.RawMessage `json:"policy"`
	Priority   int             `json:"priority"`
	Line       string          `json:"line"`
	LineNo     int             `json:"line_no,omitempty"`
}
type duplicate struct {
	Type        string       `json:"rule_type"`
	Value       string       `json:"value"`
	Count       int          `json:"count"`
	Conflict    bool         `json:"policy_conflict"`
	Occurrences []occurrence `json:"occurrences"`
}
type scanStats struct {
	Rules    int      `json:"rules_checked"`
	Rulesets int      `json:"rulesets_checked"`
	Failed   []string `json:"failed_rulesets"`
	Groups   int      `json:"duplicate_groups"`
}
type scanResult struct {
	Success    bool        `json:"success"`
	Duplicates []duplicate `json:"duplicates"`
	Stats      scanStats   `json:"stats"`
	Elapsed    float64     `json:"elapsed_time"`
}
type duplicateKey struct{ kind, value string }
type scanBucket struct {
	value string
	first occurrence
	more  []occurrence
}

// FindDuplicates retains every occurrence and first-spelling order, while
// allocating a list only for keys that actually repeat.
func (s *Service) FindDuplicates(ctx context.Context, id, host string) (scanResult, error) {
	ctx = lifecycle.Detach(ctx)
	start := s.now()
	rows, load, err := s.scanRows(ctx, id, host)
	if err != nil {
		return scanResult{}, err
	}
	index := map[duplicateKey]int{}
	buckets := []scanBucket{}
	keys := []duplicateKey{}
	result := scanResult{Success: true, Duplicates: []duplicate{}, Stats: scanStats{Failed: []string{}}}
	add := func(kind, value string, at occurrence) {
		normalized := value
		if kind != "REGEX" && kind != "REGEXP" {
			normalized = strings.ToLower(value)
		}
		key := duplicateKey{kind, normalized}
		if i, ok := index[key]; ok {
			buckets[i].more = append(buckets[i].more, at)
		} else {
			index[key] = len(buckets)
			keys = append(keys, key)
			buckets = append(buckets, scanBucket{value: value, first: at})
		}
	}
	for i, row := range rows {
		if !row.Enabled {
			continue
		}
		switch row.Kind {
		case "rule":
			kind, value := strings.ToUpper(strings.TrimSpace(row.Type)), strings.TrimSpace(row.Value)
			if kind == "" || value == "" {
				continue
			}
			result.Stats.Rules++
			add(kind, value, occurrence{SourceType: "rule", Source: msgDirectRules, RuleID: row.ID, Policy: row.Policy, Priority: i + 1, Line: kind + "," + value})
		case "ruleset":
			content, err := load(i)
			if err != nil {
				return scanResult{}, &fault{500, msgDuplicateFailed}
			}
			if content == "" {
				result.Stats.Failed = append(result.Stats.Failed, row.Name)
				continue
			}
			result.Stats.Rulesets++
			for n, line := range rulefetch.RuleLines(content) {
				kind, value, ok := shared.ParseRuleLine(line)
				if !ok || value == "" {
					continue
				}
				add(kind, value, occurrence{SourceType: "ruleset", Source: row.Name, RuleID: row.ID, Policy: row.Policy, Priority: i + 1, Line: strings.TrimSpace(line), LineNo: n + 1})
			}
		}
	}
	for i, bucket := range buckets {
		if len(bucket.more) == 0 {
			continue
		}
		items := make([]occurrence, 1, len(bucket.more)+1)
		items[0] = bucket.first
		items = append(items, bucket.more...)
		conflict := false
		for _, item := range items[1:] {
			if string(item.Policy) != string(items[0].Policy) {
				conflict = true
				break
			}
		}
		result.Duplicates = append(result.Duplicates, duplicate{Type: keys[i].kind, Value: bucket.value, Count: len(items), Conflict: conflict, Occurrences: items})
	}
	sort.SliceStable(result.Duplicates, func(i, j int) bool {
		a, b := result.Duplicates[i], result.Duplicates[j]
		if a.Conflict != b.Conflict {
			return a.Conflict
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Occurrences[0].Priority < b.Occurrences[0].Priority
	})
	result.Stats.Groups = len(result.Duplicates)
	result.Elapsed = s.elapsed(start)
	return result, nil
}
