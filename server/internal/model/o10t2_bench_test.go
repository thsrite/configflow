package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkO10T2AgentList encodes and decodes an Agent list shaped like the
// O10 snapshot (deployment journals with events and health payloads).
func BenchmarkO10T2AgentList(b *testing.B) {
	var agents []Agent
	for a := range 3 {
		var builder strings.Builder
		fmt.Fprintf(&builder, `{"id":"agent-%d","name":"a","host":"192.0.2.%d","port":8080,"deployments":{`, a, a+1)
		for d := range 60 {
			if d > 0 {
				builder.WriteByte(',')
			}
			fmt.Fprintf(&builder, `"dep-%d":{"deployment_id":"dep-%d","success":true,"status":"ok","health":{"checks":[{"name":"dns","ok":true,"detail":"%s"}]},"events":[`, d, d, strings.Repeat("x", 200))
			for e := range 8 {
				if e > 0 {
					builder.WriteByte(',')
				}
				fmt.Fprintf(&builder, `{"stage":"s%d","message":"m","at":"2026-11-21T00:00:00Z"}`, e)
			}
			builder.WriteString(`],"dns_checks":[{"name":"a","status":"ok"}]}`)
		}
		builder.WriteString(`},"ipv6_verifications":{"v":{"status":"ok","checks":[{"id":"c","ok":true}]}}}`)
		var agent Agent
		if err := json.Unmarshal([]byte(builder.String()), &agent); err != nil {
			b.Fatal(err)
		}
		agents = append(agents, agent)
	}
	encoded, err := Marshal(agents)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Marshal(agents); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encode-edited", func(b *testing.B) {
		edited := append([]Agent(nil), agents...)
		edited[0].Name = "edited"
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Marshal(edited); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for b.Loop() {
			var decoded []Agent
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("validate", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for b.Loop() {
			if err := validJSON(encoded); err != nil {
				b.Fatal(err)
			}
		}
	})
}
