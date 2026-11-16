package pyip

import (
	"math/big"
	"strconv"
)

// bigText is str(int(value)).
func bigText(v U128) string {
	if v.Hi == 0 {
		return strconv.FormatUint(v.Lo, 10)
	}
	number := new(big.Int).SetUint64(v.Hi)
	number.Lsh(number, 64)
	number.Or(number, new(big.Int).SetUint64(v.Lo))
	return number.String()
}

// Text is str(int(value)).
func (a U128) Text() string { return bigText(a) }
