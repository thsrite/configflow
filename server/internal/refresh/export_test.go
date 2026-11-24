package refresh

import (
	"configflow/internal/lifecycle"
	"context"
)

// detached keeps the admitted Runtime generation while completing request work.
func detached(ctx context.Context) context.Context { return lifecycle.Detach(ctx) }
