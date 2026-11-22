package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// referenceValidJSON and referenceParseObject are the implementations before
// O10-T2 (encoding/json Valid plus a Decoder token walk), kept verbatim as
// the oracle for the jsontext-based replacements.
func referenceValidJSON(data []byte) error {
	if !utf8.Valid(data) || !json.Valid(data) {
		return ErrJSON
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var visit func() error
	visit = func() error {
		token, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: token", ErrJSON)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return ErrJSON
				}
				name, ok := token.(string)
				if !ok || seen[name] {
					return fmt.Errorf("%w: duplicate member", ErrJSON)
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return ErrJSON
		}
		_, err = dec.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrJSON
	}
	return nil
}

func referenceParseObject(data []byte) (jsonObject, error) {
	if err := referenceValidJSON(data); err != nil {
		return jsonObject{}, err
	}
	data = bytes.Clone(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return jsonObject{}, ErrType
	}
	obj := jsonObject{data: data, opening: data[:dec.InputOffset()]}
	previous := int(dec.InputOffset())
	for dec.More() {
		keyStart := skipSpace(data, previous)
		if data[keyStart] == ',' {
			keyStart = skipSpace(data, keyStart+1)
		}
		token, err := dec.Token()
		if err != nil {
			return jsonObject{}, ErrJSON
		}
		name, ok := token.(string)
		if !ok {
			return jsonObject{}, ErrJSON
		}
		keyEnd := int(dec.InputOffset())
		valueStart := skipSpace(data, skipSpace(data, keyEnd)+1)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return jsonObject{}, ErrJSON
		}
		end := int(dec.InputOffset())
		leading := data[previous:keyStart]
		if comma := bytes.IndexByte(leading, ','); comma >= 0 {
			if len(obj.members) > 0 {
				obj.members[len(obj.members)-1].trailing = leading[:comma]
			}
			leading = leading[comma+1:]
		}
		obj.members = append(obj.members, jsonMember{name: name, key: data[keyStart:keyEnd], leading: leading, separator: data[keyEnd:valueStart], value: data[valueStart:end]})
		previous = end
	}
	if _, err := dec.Token(); err != nil {
		return jsonObject{}, ErrJSON
	}
	closingStart := skipSpace(data, previous)
	if len(obj.members) > 0 {
		obj.members[len(obj.members)-1].trailing = data[previous:closingStart]
		obj.closing = data[closingStart:]
	} else {
		obj.closing = data[previous:]
	}
	return obj, nil
}

func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error() && errors.Is(a, ErrJSON) == errors.Is(b, ErrJSON) && errors.Is(a, ErrType) == errors.Is(b, ErrType)
}

var o10t2Seeds = []string{
	`{}`, ` { "a" : 1 , "b":[1,{"c":null}] } `, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `[{"k":1,"k":2}]`,
	`{"\ud800":1,"\udc00":2}`, `{"\ud800":1,"�":2}`, "\"\xff\"", "\"\xed\xa0\x80\"", `1,2`, `{"a":1,}`,
	`{"a" 1}`, `[1,]`, `01`, `"\u12"`, `{"a":1}x`, `{"a":1} {"b":2}`, `  `, `nul`, `-0.5e+3`, `[]`, `"x"`,
	"{\"a\":1 \n,\"b\":\"x\" \t}", `{"":1,"":2}`, `{"a\/b":1,"a/b":2}`, `{"a":[{"a":1},{"a":1}]}`,
	strings.Repeat("[", 10000) + strings.Repeat("]", 10000), strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	strings.Repeat(`{"a":`, 10000) + "1" + strings.Repeat("}", 10000) + `,"a":2}`,
}

func FuzzValidJSONReference(f *testing.F) {
	for _, seed := range o10t2Seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		want := referenceValidJSON(data)
		if got := validJSON(data); !sameError(got, want) {
			t.Fatalf("validJSON(%q) = %v, reference %v", data, got, want)
		}
	})
}

func FuzzParseObjectReference(f *testing.F) {
	for _, seed := range o10t2Seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		want, wantErr := referenceParseObject(data)
		got, gotErr := parseObject(data)
		if !sameError(gotErr, wantErr) {
			t.Fatalf("parseObject(%q) error %v, reference %v", data, gotErr, wantErr)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parseObject(%q) = %#v, reference %#v", data, got, want)
		}
		if wantErr != nil {
			return
		}
		// splitArray must agree with json.Unmarshal into []json.RawMessage.
		for _, member := range want.members {
			var items []json.RawMessage
			reference := json.Unmarshal(member.value, &items)
			split, err := splitArray(member.value)
			if (err == nil) != (reference == nil) || (err == nil && (items == nil) != (split == nil)) || !reflect.DeepEqual(normalizeRaw(split), normalizeRaw(items)) {
				t.Fatalf("splitArray(%q) = %q %v, json.Unmarshal %q %v", member.value, split, err, items, reference)
			}
		}
	})
}

func normalizeRaw(items []json.RawMessage) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item)
	}
	return out
}

// FuzzMarshalOutputValid decodes fuzzed Agents, applies edits that reach
// every encoder path (records, OrderedMap, foreign RawMessage, clears) and
// checks that every successful encoding is complete, duplicate-free JSON and
// that Marshal and a direct MarshalJSON call agree byte for byte.
func FuzzMarshalOutputValid(f *testing.F) {
	f.Add([]byte(`{"id":"a","deployments":{"d":{"deployment_id":"d","health":{"x":1},"events":[{"stage":"s"}]}},"logging_config":{"enabled":true},"future":{"a":[1,2]}}`), []byte(`{"a":1}`), uint8(0))
	f.Add([]byte(`{"id":"a","port":"x","ipv6_verifications":{"v":{"status":"ok"},"w":false}}`), []byte(`1,2`), uint8(3))
	f.Add([]byte(` { "name" : "例", "rule_sync":{"settings":{"enabled":true}} } `), []byte(`{"a":1,"a":2}`), uint8(6))
	f.Fuzz(func(t *testing.T, data, value []byte, edit uint8) {
		var agent Agent
		if requireObject(data) != nil || json.Unmarshal(data, &agent) != nil {
			return
		}
		check := func(label string, record Agent) {
			t.Helper()
			marshaled, marshalErr := Marshal(record)
			direct, directErr := record.MarshalJSON()
			if marshalErr != nil {
				if directErr == nil && validJSON(direct) == nil {
					t.Fatalf("%s: Marshal failed (%v) but MarshalJSON returned valid output", label, marshalErr)
				}
				return
			}
			if directErr != nil || !bytes.Equal(marshaled, direct) {
				t.Fatalf("%s: Marshal and MarshalJSON differ: %v", label, directErr)
			}
			if err := referenceValidJSON(marshaled); err != nil {
				t.Fatalf("%s: output rejected by reference validator: %v", label, err)
			}
			fuzzUniqueJSON(t, marshaled)
		}
		check("unchanged", agent)
		edited := agent
		switch edit % 8 {
		case 0:
			edited.Name = string(value)
		case 1:
			_ = SetStoredMember(&edited, "future", value)
		case 2:
			_ = SetStoredMember(&edited, "deployments", value)
		case 3:
			deployment, _ := edited.Deployments.Get("d")
			deployment.Health = value
			edited.Deployments.Set("d", deployment)
		case 4:
			edited.LoggingConfig = &AgentLoggingConfig{Enabled: value}
		case 5:
			_ = SetStoredMember(&edited, "logging_config", value)
		case 6:
			_ = ZeroMember(&edited, "deployments")
			edited.LatestDeployment = &Deployment{Health: value}
		case 7:
			_ = SetStoredMember(&edited, "ipv6_verifications", value)
		}
		check("edited", edited)
	})
}
