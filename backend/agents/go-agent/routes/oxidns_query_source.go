package routes

import (
	"encoding/json"
	"strconv"
	"strings"
)

type oxSourceStep struct {
	Sequence string `json:"sequence_tag"`
	Node     *int   `json:"node_index"`
	Kind     string `json:"kind"`
	Tag      string `json:"tag"`
	Outcome  string `json:"outcome"`
}

type oxSourceRecord struct {
	ID          int64           `json:"id"`
	Created     int64           `json:"created_at_ms"`
	RequestID   uint16          `json:"request_id"`
	Client      string          `json:"client_ip"`
	Questions   json.RawMessage `json:"questions_json"`
	HasResponse bool            `json:"has_response"`
	Rcode       json.RawMessage `json:"rcode"`
	AnswerCount json.RawMessage `json:"answer_count"`
	Error       *string         `json:"error"`
	Steps       []oxSourceStep  `json:"steps"`
}

func oxSourceQuick(tag, kind string) bool {
	return strings.HasPrefix(tag, "qs.") && strings.HasSuffix(tag, "."+kind)
}

// The official API emits text DNS types/codes; tolerate their equivalent numeric
// JSON forms without treating missing, null, boolean or object values as zero.
func oxSourceToken(raw json.RawMessage, number float64, names ...string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value := value.(type) {
	case float64:
		return value == number
	case string:
		value = strings.ToUpper(strings.TrimSpace(value))
		for _, name := range names {
			if value == name {
				return true
			}
		}
	}
	return false
}

func oxSourceIPv4Selector(step oxSourceStep) bool {
	return step.Node != nil && *step.Node >= 0 && step.Sequence != "" &&
		step.Tag == "qs.exec."+step.Sequence+"."+strconv.Itoa(*step.Node)+".prefer_ipv4"
}

func oxSourceIPv4SuppressedResponse(record oxSourceRecord) bool {
	var questions []struct {
		Qtype json.RawMessage `json:"qtype"`
	}
	if json.Unmarshal(record.Questions, &questions) != nil || len(questions) != 1 ||
		!oxSourceToken(questions[0].Qtype, 28, "AAAA", "28") ||
		!oxSourceToken(record.Rcode, 0, "NO ERROR", "NOERROR", "0") {
		return false
	}
	// No string aliases here: answer_count must be explicitly numeric zero.
	return oxSourceToken(record.AnswerCount, 0)
}

// These are ConfigFlow's standard path labels, not the server selected inside a
// forward group. Keep the fallback classifier in oxidnsQuerySource.ts in sync.
func oxQuerySource(record oxSourceRecord) string {
	if record.Error != nil && *record.Error != "" {
		return "error"
	}
	if !record.HasResponse {
		return "no_response"
	}
	if len(record.Steps) == 0 {
		return "unrecorded"
	}
	source := "unknown"
	calls := []int{}
	for i, step := range record.Steps {
		if step.Kind == "builtin" && step.Tag == "reject" && step.Outcome == "stop" {
			source = "rejected"
		}
		if step.Kind != "executor" {
			continue
		}
		if step.Outcome == "entered" {
			calls = append(calls, i)
			continue
		}
		callIndex := len(calls) - 1
		for callIndex >= 0 {
			entered := record.Steps[calls[callIndex]]
			sameNode := entered.Node == nil && step.Node == nil || entered.Node != nil && step.Node != nil && *entered.Node == *step.Node
			if entered.Tag == step.Tag && entered.Sequence == step.Sequence && sameNode {
				break
			}
			callIndex--
		}
		if callIndex < 0 {
			continue
		}
		start := calls[callIndex]
		calls = append(calls[:callIndex], calls[callIndex+1:]...)
		if step.Outcome != "next" && step.Outcome != "stop" && step.Outcome != "return" {
			continue
		}
		switch step.Tag {
		case "forward_local":
			source = "local"
		case "forward_remote":
			source = "remote"
		case "forward_fallback":
			source = "fallback"
		case "adguard_block", "cf_dns_blackhole":
			source = "blocked"
		case "custom_hosts":
			if i+1 < len(record.Steps) {
				check := record.Steps[i+1]
				if check.Kind == "matcher" && oxSourceQuick(check.Tag, "has_resp") && check.Outcome == "matched" {
					source = "hosts"
				}
			}
		case "lazy_cache":
			// A miss wraps downstream execution; its completion is not a hit.
			if step.Outcome == "stop" && i == start+1 {
				source = "cache"
			}
		case "local_probe_cache":
			if step.Sequence == "local_probe" && step.Outcome == "return" && i == start+3 {
				check, end := record.Steps[start+1], record.Steps[start+2]
				if check.Kind == "matcher" && oxSourceQuick(check.Tag, "has_resp") && check.Outcome == "not_matched" && end.Kind == "builtin" && end.Tag == "return" && end.Outcome == "return" {
					source = "cache"
				}
			}
		default:
			if strings.HasPrefix(step.Tag, "cf_dns_fwd_") {
				source = "pinned"
			} else if strings.HasPrefix(step.Tag, "cf_dns_up_") || strings.HasPrefix(step.Tag, "cf_dns_cache_") {
				// A rule's pinned upstream chain and its private cache. As with
				// lazy_cache, a wrapping completion is not evidence by itself: the
				// inner forward, or the reject on no answer, decides instead.
				if i == start+1 {
					source = "pinned"
				}
			} else if oxSourceIPv4Selector(step) && step.Outcome == "stop" &&
				i == start+1 && oxSourceIPv4SuppressedResponse(record) {
				// Suppression retains only the selector's entered/stop pair. Its
				// isolated probe may still have contacted upstream DNS servers.
				source = "ipv4_preferred"
			} else if step.Tag == "drop_resp" || oxSourceQuick(step.Tag, "drop_resp") {
				source = "unknown"
			} else if oxSourceQuick(step.Tag, "black_hole") && step.Outcome == "stop" && i == start+1 {
				// Blocked query types answer with an inline black_hole; earlier
				// drafts used the builtin reject 0, labelled the same way.
				source = "rejected"
			}
		}
	}
	return source
}
