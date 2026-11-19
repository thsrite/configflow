package trafficstore

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
)

type storedCounter struct {
	integer *big.Int
	real    float64
}

func parseCounter(raw json.RawMessage) (storedCounter, error) {
	if string(raw) == "true" {
		return storedCounter{integer: big.NewInt(1)}, nil
	}
	if string(raw) == "false" {
		return storedCounter{integer: new(big.Int)}, nil
	}
	if len(raw) == 0 || raw[0] == '"' {
		return storedCounter{}, &TypeError{Message: "invalid traffic counter"}
	}
	if !bytes.ContainsAny(raw, ".eE") {
		if n, ok := new(big.Int).SetString(string(raw), 10); ok {
			return storedCounter{integer: n}, nil
		}
	}
	var value json.Number
	if err := jsondecode.Unmarshal(raw, &value); err != nil {
		return storedCounter{}, &TypeError{Message: "invalid traffic counter"}
	}
	f, err := value.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return storedCounter{}, &TypeError{Message: "invalid traffic counter"}
	}
	return storedCounter{real: f}, nil
}
func (n storedCounter) float() float64 {
	if n.integer == nil {
		return n.real
	}
	f, _ := new(big.Float).SetInt(n.integer).Float64()
	return f
}
func (n storedCounter) add(v storedCounter) (storedCounter, error) {
	if n.integer != nil && v.integer != nil {
		return storedCounter{integer: new(big.Int).Add(n.integer, v.integer)}, nil
	}
	sum := n.float() + v.float()
	if math.IsInf(sum, 0) || math.IsNaN(sum) {
		return storedCounter{}, &TypeError{Message: "traffic counter overflow"}
	}
	return storedCounter{real: sum}, nil
}
func (n storedCounter) raw() json.RawMessage {
	if n.integer != nil {
		return json.RawMessage(n.integer.String())
	}
	return json.RawMessage(strconv.FormatFloat(n.real, 'g', -1, 64))
}
func (n storedCounter) rat() *big.Rat {
	if n.integer != nil {
		return new(big.Rat).SetInt(n.integer)
	}
	return new(big.Rat).SetFloat64(n.real)
}
