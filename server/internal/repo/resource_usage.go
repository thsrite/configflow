package repo

import (
	"context"
	"slices"

	"configflow/internal/model"
)

// Usage identifies a shared resource consumer without credentials.
type Usage struct {
	Kind        string  `json:"kind"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	ProfileID   *string `json:"profile_id,omitempty"`
	ProfileName *string `json:"profile_name,omitempty"`
}

// ResourceUsage returns native public usage records from one generation.
func (r *Repository) ResourceUsage(ctx context.Context, kind, id string) ([]Usage, error) {
	if !slices.Contains(SharedFields, kind) {
		return nil, validationError(msgUnknownResourceType)
	}
	var result []Usage
	err := r.View(ctx, func(document *model.Document) error {
		var err error
		result, err = resourceUsages(document, kind, id)
		return err
	})
	return result, err
}
