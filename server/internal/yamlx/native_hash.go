package yamlx

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValueHash hashes a validated node's effective value, ignoring presentation,
// mapping order and alias identity. It follows Equal's type and numeric rules.
// A memo preserves alias sharing; neither display clipping nor depth limits can
// erase changes. As with Get and Equal, callers parse untrusted input first.
func ValueHash(n *Node) [sha256.Size]byte {
	memo := map[*Node][sha256.Size]byte{}
	var visit func(*Node) [sha256.Size]byte
	visit = func(n *Node) [sha256.Size]byte {
		n = Resolve(n)
		if hash, ok := memo[n]; ok {
			return hash
		}
		h := sha256.New()
		if n == nil {
			return sha256.Sum256(nil)
		}
		// Length-prefixed scalar components and fixed-size child hashes keep
		// boundaries unambiguous, including NULs and collection-valued keys.
		write := func(s string) {
			_ = binary.Write(h, binary.BigEndian, uint64(len(s)))
			_, _ = h.Write([]byte(s))
		}
		_ = binary.Write(h, binary.BigEndian, uint32(n.Kind))
		write(n.ShortTag())
		switch n.Kind {
		case yaml.MappingNode:
			entries := [][2][sha256.Size]byte{}
			for key, value := range AllPairs(n) {
				entries = append(entries, [2][sha256.Size]byte{visit(key), visit(value)})
			}
			slices.SortFunc(entries, func(a, b [2][sha256.Size]byte) int {
				if c := bytes.Compare(a[0][:], b[0][:]); c != 0 {
					return c
				}
				return bytes.Compare(a[1][:], b[1][:])
			})
			for _, entry := range entries {
				_, _ = h.Write(entry[0][:])
				_, _ = h.Write(entry[1][:])
			}
		case yaml.SequenceNode, yaml.DocumentNode:
			for _, child := range n.Content {
				hash := visit(child)
				_, _ = h.Write(hash[:])
			}
		case yaml.ScalarNode:
			switch n.ShortTag() {
			case "!!null":
			case "!!bool":
				write(strings.ToLower(n.Value))
			case "!!int":
				if v, ok := yamlInteger(n.Value); ok {
					write(v.String())
				} else {
					write(n.Value)
				}
			case "!!float":
				write(floatKey(n.Value))
			default:
				write(n.Value)
			}
		default:
			write(n.Value)
		}
		var hash [sha256.Size]byte
		copy(hash[:], h.Sum(nil))
		memo[n] = hash
		return hash
	}
	return visit(n)
}
