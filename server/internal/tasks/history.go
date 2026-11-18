package tasks

import (
	"configflow/internal/taskstatus"
	"encoding/json"
	"math/big"
)

func historyCount(raw json.RawMessage) (*big.Int, error) { return taskstatus.Count(raw) }
