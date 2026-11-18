package ipv6

// Legacy repo, converter and registry types stop at this adapter. The IPv6
// owners only operate on native snapshots and independently owned Agent records.
import (
	"configflow/internal/convert"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

func defaultMihomo() (*yamlx.Node, error) { return yamlbridge.Encode(convert.DefaultMihomoTemplate()) }
