package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

var requestNative = func(ctx context.Context, m Registry, a *model.Agent, endpoint string, params query, minimum string) (model.Fields, error) {
	if codec, ok := m.(interface {
		monitorReply(context.Context, *model.Agent, string, query, string) (model.Fields, error)
	}); ok {
		return codec.monitorReply(ctx, a, endpoint, params, minimum)
	}
	return monitorRequest(ctx, m, a, endpoint, params, minimum)
}

func readDNSAgent(ctx context.Context, m Registry, id, minimum string, history bool) (*model.Agent, error) {
	a, err := m.ReadAgent(ctx, id, history)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, NewError("AGENT_NOT_FOUND", 404, msgAgentMissing)
	}
	if originalText(a.MemberJSON("service_type")) != "oxidns" {
		return nil, NewError("WRONG_SERVICE", 400, msgDNSWrongService)
	}
	if !modelVersionAtLeast(a, minimum) {
		return nil, upgradeRequired(minimum)
	}
	return a, nil
}
func monitorRequest(ctx context.Context, m Registry, a *model.Agent, endpoint string, params query, minimum string) (model.Fields, error) {
	var fields model.Fields
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	origin, err := Origin(a)
	if err != nil {
		return fields, err
	}
	token, valid := originalString(a.MemberJSON("token"))
	if !valid {
		return fields, unreachable()
	}
	header, err := monitorHeaders(token)
	if err != nil {
		return fields, unreachable()
	}
	url := origin + "/api/oxidns/" + endpoint
	if len(params) > 0 {
		url += "?" + params.encode()
	}
	if endpoint == "queries" {
		header.Set("X-ConfigFlow-Query-Sources", "1")
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: http.MethodGet, URL: url, Header: header, Timeout: agents.Pair(3, 12), NoProxy: true, NoRedirects: true, MaxBody: MaxResponseBytes})
	if err != nil {
		return fields, unreachable()
	}
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	fields, err = protocolAnswer(response, MaxResponseBytes, func(code string, status int) *Error {
		if code == "UPGRADE_REQUIRED" {
			return upgradeRequired(minimum)
		}
		return NewError(code, status, "")
	})
	if err != nil {
		return fields, err
	}
	failed := response.StatusCode >= 400
	for _, key := range []string{"success", "ok"} {
		flag, present, valid := answerBool(fields, key)
		if present && !valid {
			return fields, NewError("INVALID_RESPONSE", 0, "")
		}
		if present && !flag {
			failed = true
		}
	}
	if !failed {
		return fields, nil
	}
	rawCode, _ := fields.Get("code")
	if len(rawCode) > 0 && (rawCode[0] == '[' || rawCode[0] == '{') {
		return fields, unreachable()
	}
	code := fieldText(fields, "code")
	if _, known := Messages[code]; !known {
		code = "CORE_UNREACHABLE"
		if response.StatusCode == 404 {
			code = "UPGRADE_REQUIRED"
		}
	}
	status := 502
	switch code {
	case "RECORD_NOT_FOUND":
		status = 404
	case "INVALID_QUERY":
		status = 400
	case "UPGRADE_REQUIRED", "API_NOT_CONFIGURED", "RECORDER_NOT_CONFIGURED", "TLS_UNSUPPORTED", "CONFIG_UNSUPPORTED", "SNAPSHOT_CHANGED":
		status = 409
	}
	if code == "UPGRADE_REQUIRED" {
		return fields, upgradeRequired(minimum)
	}
	return fields, NewError(code, status, "")
}

func protocolInt(fields model.Fields, key string, minimum, maximum int64) (int64, bool) {
	raw, present := fields.Get(key)
	if !present {
		return 0, false
	}
	var value *int64
	if jsondecode.Unmarshal(raw, &value) != nil || value == nil || *value < minimum || *value > maximum {
		return 0, false
	}
	return *value, true
}
func protocolNumber(raw json.RawMessage, maximum int64) *MetricNumber {
	var value MetricNumber
	if jsondecode.Unmarshal(raw, &value) != nil || value.num.less(intNum(0)) || intNum(maximum).less(value.num) {
		return nil
	}
	finite, err := value.isFinite()
	if err != nil || !finite {
		return nil
	}
	return &value
}
func requiredBool(fields model.Fields, key string, value bool) bool {
	flag, present, valid := answerBool(fields, key)
	return present && valid && flag == value
}

var recordFields = []string{"id", "created_at_ms", "elapsed_ms", "client_ip", "questions_json", "error", "has_response",
	"rcode", "answer_count", "authority_count", "additional_count", "answers_json", "authorities_json", "additionals_json",
	"signature_json", "request_id", "req_rd", "req_cd", "req_ad", "req_opcode", "resp_aa", "resp_tc", "resp_ra", "resp_ad", "resp_cd"}

func queryRecord(raw json.RawMessage, detail bool) (json.RawMessage, error) {
	invalid := NewError("INVALID_RESPONSE", 0, "")
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return nil, invalid
	}
	if _, valid := protocolInt(fields, "id", 1, MaxInteger); !valid {
		return nil, invalid
	}
	for _, key := range []string{"created_at_ms", "elapsed_ms"} {
		raw, _ := fields.Get(key)
		if protocolNumber(raw, MaxInteger) == nil {
			return nil, invalid
		}
	}
	for _, key := range []string{"questions_json", "answers_json"} {
		raw, _ := fields.Get(key)
		var list []json.RawMessage
		if jsondecode.Unmarshal(raw, &list) != nil || list == nil {
			return nil, invalid
		}
	}
	var output model.Fields
	for _, key := range recordFields {
		if value, present := fields.Get(key); present {
			_ = output.Set(key, value)
		}
	}
	if detail {
		raw, _ := fields.Get("steps")
		var steps []json.RawMessage
		if jsondecode.Unmarshal(raw, &steps) != nil || steps == nil {
			return nil, invalid
		}
		_ = output.Set("steps", raw)
	}
	if source := fieldText(fields, "resolution_source"); resolutionSources[source] {
		raw, _ := fields.Get("resolution_source")
		_ = output.Set("resolution_source", raw)
	}
	return json.Marshal(output)
}

// QueryList is a sanitized page of bounded DNS query records.
type QueryList struct {
	Success    bool              `json:"success"`
	Recorder   string            `json:"recorder"`
	Records    []json.RawMessage `json:"records"`
	NextCursor *string           `json:"next_cursor"`
}

// QueryDetail contains one sanitized record and its resolution steps.
type QueryDetail struct {
	Success  bool            `json:"success"`
	Recorder string          `json:"recorder"`
	Record   json.RawMessage `json:"record"`
}

// ClientCount is one validated client IP and its sample count.
type ClientCount struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}

// ClientList lists the clients in one complete bounded sample.
type ClientList struct {
	Success       bool          `json:"success"`
	Recorder      string        `json:"recorder"`
	Clients       []ClientCount `json:"clients"`
	SampleSize    int64         `json:"sample_size"`
	SampleLimited bool          `json:"sample_limited"`
}

// ReadQueries validates filters and returns one bounded page from the Agent.
func ReadQueries(ctx context.Context, m Registry, id string, args [][2]string) (*QueryList, error) {
	params, err := QueryParams(args, false)
	if err != nil {
		return nil, err
	}
	minimum := MinAgentVersion
	if ip, _ := params.get("client_ip"); ip != "" {
		minimum = MinClientsAgentVersion
	}
	a, err := readDNSAgent(ctx, m, id, minimum, false)
	if err != nil {
		return nil, err
	}
	fields, err := requestNative(ctx, m, a, "queries", params, minimum)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	raw, _ := fields.Get("records")
	var rows []json.RawMessage
	if !requiredBool(fields, "ok", true) || jsondecode.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 100 {
		return nil, invalid
	}
	raw, present := fields.Get("next_cursor")
	var cursor *string
	if present && (jsondecode.Unmarshal(raw, &cursor) != nil || cursor != nil && !cursorRE.MatchString(*cursor)) {
		return nil, invalid
	}
	recorder, _ := params.get("recorder")
	result := &QueryList{Success: true, Recorder: recorder, Records: make([]json.RawMessage, 0, len(rows)), NextCursor: cursor}
	for _, row := range rows {
		record, err := queryRecord(row, false)
		if err != nil {
			return nil, err
		}
		result.Records = append(result.Records, record)
	}
	return result, nil
}

// ReadQuery reads one record with its detail steps.
func ReadQuery(ctx context.Context, m Registry, id, recordID string, args [][2]string) (*QueryDetail, error) {
	params, err := QueryParams(args, true)
	if err != nil {
		return nil, err
	}
	number, parseErr := strconv.ParseInt(recordID, 10, 64)
	if !recordIDRE.MatchString(recordID) || parseErr != nil || number > MaxInteger {
		return nil, invalidQuery()
	}
	a, err := readDNSAgent(ctx, m, id, MinAgentVersion, false)
	if err != nil {
		return nil, err
	}
	fields, err := requestNative(ctx, m, a, "queries/"+recordID, params, MinAgentVersion)
	if err != nil {
		return nil, err
	}
	if !requiredBool(fields, "ok", true) {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	raw, _ := fields.Get("record")
	record, err := queryRecord(raw, true)
	if err != nil {
		return nil, err
	}
	recorder, _ := params.get("recorder")
	return &QueryDetail{Success: true, Recorder: recorder, Record: record}, nil
}

// ReadClients validates and totals the Agent's client sample.
func ReadClients(ctx context.Context, m Registry, id string, args [][2]string) (*ClientList, error) {
	params, err := ClientParams(args)
	if err != nil {
		return nil, err
	}
	a, err := readDNSAgent(ctx, m, id, MinClientsAgentVersion, false)
	if err != nil {
		return nil, err
	}
	fields, err := requestNative(ctx, m, a, "clients", params, MinClientsAgentVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	recorder := fieldText(fields, "recorder")
	selected, hasParam := params.get("recorder")
	sample, valid := protocolInt(fields, "sample_size", 0, ClientSampleLimit)
	raw, _ := fields.Get("rows")
	var rows []json.RawMessage
	if !requiredBool(fields, "ok", true) || !validRecorderTag(recorder) || hasParam && selected != recorder || !valid || jsondecode.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > ClientSampleLimit {
		return nil, invalid
	}
	result := &ClientList{Success: true, Recorder: recorder, Clients: make([]ClientCount, 0, len(rows)), SampleSize: sample, SampleLimited: sample == ClientSampleLimit}
	seen := map[string]bool{}
	total := int64(0)
	for _, raw := range rows {
		var row model.Fields
		if jsondecode.Unmarshal(raw, &row) != nil {
			return nil, invalid
		}
		ip := fieldText(row, "key")
		count, valid := protocolInt(row, "count", 1, sample)
		if strings.Contains(ip, "%") || !isPlainIP(ip) || seen[ip] || !valid {
			return nil, invalid
		}
		seen[ip] = true
		total += count
		result.Clients = append(result.Clients, ClientCount{ip, count})
	}
	if total != sample {
		return nil, invalid
	}
	return result, nil
}
