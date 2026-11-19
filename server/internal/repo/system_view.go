package repo

import (
	"context"
	"sync/atomic"

	"configflow/internal/model"
)

type systemViewKey struct{}
type systemView struct {
	repository *Repository
	system     *model.System
	active     atomic.Bool
}

// editSystemView makes a section updater's proposed system visible to nested
// read-only readers. Its active lifetime ends with the callback, including an
// abort; an escaped context cannot keep reading a discarded proposal.
func (r *Repository) editSystemView(ctx context.Context, system *model.System, edit func(context.Context, *model.System) error) error {
	view := &systemView{repository: r, system: system}
	view.active.Store(true)
	defer view.active.Store(false)
	return edit(context.WithValue(ctx, systemViewKey{}, view), system)
}

func (r *Repository) systemInView(ctx context.Context, document *model.Document) *model.Document {
	view, _ := ctx.Value(systemViewKey{}).(*systemView)
	if view == nil || view.repository != r || !view.active.Load() {
		return document
	}
	projected := *document
	projected.System = *view.system
	return &projected
}
