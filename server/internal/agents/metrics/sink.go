package metrics

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
)

type metricsSink struct{}

func (metricsSink) AddMetricsJSON(ctx context.Context, m *agents.Manager, agentID string, sample json.RawMessage) error {
	return addMetricsJSON(ctx, m, agentID, sample)
}

func (metricsSink) DeleteMetricsHistory(ctx context.Context, m *agents.Manager, agentID string) error {
	return deleteHistoryContext(ctx, m, agentID)
}

func (metricsSink) DeleteTrafficHistory(ctx context.Context, m *agents.Manager, agentID string) error {
	return deleteTrafficHistoryContext(ctx, m, agentID)
}

// MetricsSink returns an agents.MetricsSink backed by the metrics and traffic
// history owners.
func MetricsSink() agents.MetricsSink { return metricsSink{} }

// TrafficStoreDeleter returns the NamedDeleter that clears an Agent's traffic
// store after deletion.
func TrafficStoreDeleter() agents.NamedDeleter {
	return agents.NamedDeleter{Name: "traffic_store", Delete: deleteTrafficStore}
}
