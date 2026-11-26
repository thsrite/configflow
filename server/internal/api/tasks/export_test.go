package tasks

import (
	"configflow/internal/tasks"
)

// NewHandler constructs a Handler for reg.
func NewHandler(reg *tasks.Registry) *Handler {
	return &Handler{Tasks: reg}
}
