package notifications

// TODO(N8-9): remove these transcoders and hook adapters as owners migrate.
import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

func decodeLegacy[T any](value any) (*T, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := new(T)
	if err = json.Unmarshal(data, result); err != nil {
		return nil, err
	}
	return result, nil
}
func legacyObject(value any) (*jsonx.Object, error) { return decodeLegacy[jsonx.Object](value) }
func legacyItems(values []*Item, err error) ([]any, error) {
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		item, err := legacyObject(value)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// Sources is the compatibility registration surface of unconverted owners.
type Sources struct {
	ListTasks    func(context.Context) ([]*jsonx.Object, error)
	MissingTasks func() []string
	Agents       func(context.Context) ([]*jsonx.Object, error)
	Summarize    func(*jsonx.Object, *jsonx.Object) *jsonx.Object
}

// SetSources installs native codecs around old hooks.
func SetSources(s Sources) func() {
	var input Inputs
	input.MissingTasks = s.MissingTasks
	if s.ListTasks != nil {
		input.Rows = func(ctx context.Context) ([]*tasks.Task, error) {
			values, err := s.ListTasks(ctx)
			if err != nil {
				return nil, err
			}
			rows, err := decodeLegacy[[]*tasks.Task](values)
			if err != nil {
				return nil, err
			}
			return *rows, nil
		}
	}
	if s.Agents != nil {
		input.Agents = func(ctx context.Context) ([]model.Agent, error) {
			values, err := s.Agents(ctx)
			if err != nil {
				return nil, err
			}
			agents, err := decodeLegacy[[]model.Agent](values)
			if err != nil {
				return nil, err
			}
			return *agents, nil
		}
	}
	if s.Summarize != nil {
		input.Summarize = func(report *RouterReport, plan json.RawMessage) (*RouterSummary, error) {
			left, err := legacyObject(report)
			if err != nil {
				return nil, err
			}
			right, err := legacyObject(plan)
			if err != nil {
				return nil, err
			}
			return decodeLegacy[RouterSummary](s.Summarize(left, right))
		}
	}
	return SetInputs(input)
}

// ResetSources clears compatibility source wiring.
func ResetSources() { ResetInputs() }
func currentSources() Sources {
	s := currentInputs()
	result := Sources{MissingTasks: s.MissingTasks}
	if s.Rows != nil {
		result.ListTasks = func(ctx context.Context) ([]*jsonx.Object, error) {
			rows, err := s.Rows(ctx)
			if err != nil {
				return nil, err
			}
			out, err := decodeLegacy[[]*jsonx.Object](rows)
			if err != nil {
				return nil, err
			}
			return *out, nil
		}
	}
	if s.Agents != nil {
		result.Agents = func(ctx context.Context) ([]*jsonx.Object, error) {
			agents, err := s.Agents(ctx)
			if err != nil {
				return nil, err
			}
			out, err := decodeLegacy[[]*jsonx.Object](agents)
			if err != nil {
				return nil, err
			}
			return *out, nil
		}
	}
	if s.Summarize != nil {
		result.Summarize = func(report, plan *jsonx.Object) *jsonx.Object {
			input, err := decodeLegacy[RouterReport](report)
			if err != nil {
				return nil
			}
			raw, err := json.Marshal(plan)
			if err != nil {
				return nil
			}
			summary, err := s.Summarize(input, raw)
			if err != nil {
				return nil
			}
			out, _ := legacyObject(summary)
			return out
		}
	}
	return result
}

// Target adapts old destination field packing.
func Target(path, label string, query ...any) *jsonx.Object {
	values := map[string]string{}
	for i := 0; i+1 < len(query); i += 2 {
		key, _ := query[i].(string)
		if query[i+1] != nil {
			values[key] = fmt.Sprint(query[i+1])
		}
	}
	result, _ := legacyObject(Destination(path, label, values))
	return result
}

// EventMessage adapts old optional diagnostic fields.
func EventMessage(value any, secrets ...any) string { return diag.EventMessage(value, secrets) }

// Fingerprint adapts a legacy incident to native D2 identity encoding.
func Fingerprint(key string, value *jsonx.Object) string {
	item, err := decodeLegacy[Item](value)
	if err != nil {
		return ""
	}
	result, _ := IncidentFingerprint(key, item)
	return result
}

// RouterSignature adapts the old encoded configuration pair to native D2 encoding.
func RouterSignature(config, router *jsonx.Object) string {
	a, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	b, err := json.Marshal(router)
	if err != nil {
		return ""
	}
	result, _ := RouterDigest(a, b)
	return result
}

// Transaction adapts an old store callback.
func Transaction(dataDir string, fn func(*jsonx.Object) error) error {
	return TransactionContext(context.Background(), dataDir, fn)
}

// TransactionContext retains cancellation checks in the native leaf transaction.
func TransactionContext(ctx context.Context, dataDir string, fn func(*jsonx.Object) error) error {
	return UpdateItems(ctx, dataDir, func(items *Items) error {
		wire, err := legacyObject(items)
		if err != nil {
			return err
		}
		if err = fn(wire); err != nil {
			return err
		}
		value, err := decodeLegacy[Items](wire)
		if err != nil {
			return err
		}
		*items = *value
		return nil
	})
}

// MarkRead adapts displayed legacy versions.
func MarkRead(dataDir string, versions *jsonx.Object) error {
	return MarkReadContext(context.Background(), dataDir, versions)
}

// MarkReadContext transcodes versions before entering the native leaf transaction.
func MarkReadContext(ctx context.Context, dataDir string, versions *jsonx.Object) error {
	values, err := decodeLegacy[map[string]string](versions)
	if err != nil {
		return err
	}
	return Dismiss(ctx, dataDir, *values)
}

// RecordAgent adapts an unconverted deployment owner.
func RecordAgent(ctx context.Context, agent *jsonx.Object, repository *repo.Repository) error {
	value, err := decodeLegacy[*model.Agent](agent)
	if err != nil {
		return err
	}
	return SaveAgent(ctx, *value, repository)
}

// RecordRegistrationRejection adapts an unconverted registration owner.
func RecordRegistrationRejection(ctx context.Context, repository *repo.Repository, data *jsonx.Object, reason string) error {
	value, err := decodeLegacy[*model.Agent](data)
	if err != nil {
		return err
	}
	return RejectRegistration(ctx, repository, *value, reason)
}

// RecordAgentAddress adapts an unconverted registry owner.
func RecordAgentAddress(ctx context.Context, repository *repo.Repository, agent *jsonx.Object, host, label, message string) error {
	value, err := decodeLegacy[model.Agent](agent)
	if err != nil {
		return err
	}
	return SaveAgentAddress(ctx, repository, value, host, label, message)
}

// RecordNotice adapts an old destination.
func RecordNotice(ctx context.Context, repository *repo.Repository, key, title, message string, link *jsonx.Object) error {
	value, err := decodeLegacy[*Navigation](link)
	if err != nil {
		return err
	}
	return SaveNotice(ctx, repository, key, title, message, *value)
}

// RecordRouter adapts driver snapshots, retaining the full plan for its owner hook.
func RecordRouter(ctx context.Context, profileID string, config, router, report, plan *jsonx.Object, checkedAt string) error {
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	value, err := decodeLegacy[model.Router](router)
	if err != nil {
		return err
	}
	nativeReport, err := decodeLegacy[RouterReport](report)
	if err != nil {
		return err
	}
	encodedPlan, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return SaveRouter(ctx, profileID, encoded, value, nativeReport, encodedPlan, checkedAt)
}

// Snapshot adapts pending native incidents for unconverted consumers.
func Snapshot(ctx context.Context, completedTask string) ([]any, error) {
	return legacyItems(SnapshotItems(ctx, completedTask))
}

// Preview adapts the native read-only projection.
func Preview(ctx context.Context) ([]any, error) { return legacyItems(PreviewItems(ctx)) }
