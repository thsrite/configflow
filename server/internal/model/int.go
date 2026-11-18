package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Int is an int64 stored as an integral JSON number. Decimal and exponent
// notation and strict ASCII decimal integer strings are accepted exactly;
// booleans, fractional values and overflow are not.
type Int int64

// UnmarshalJSON decodes an integral int64 without passing through float64.
func (n *Int) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return fmt.Errorf("%w: integer text: %w", ErrType, err)
		}
		digits := strings.TrimPrefix(text, "-")
		if digits == "" {
			return fmt.Errorf("%w: decimal integer required", ErrType)
		}
		for _, digit := range []byte(digits) {
			if digit < '0' || digit > '9' {
				return fmt.Errorf("%w: decimal integer required", ErrType)
			}
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: integer range: %w", ErrType, err)
		}
		*n = Int(value)
		return nil
	}
	if len(data) == 0 || !json.Valid(data) || (data[0] != '-' && (data[0] < '0' || data[0] > '9')) {
		return fmt.Errorf("%w: integral number required", ErrType)
	}
	text := string(data)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	mantissa, exponentText, hasExponent := text, "", false
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		mantissa, exponentText, hasExponent = text[:at], text[at+1:], true
	}
	fraction := int64(0)
	if at := strings.IndexByte(mantissa, '.'); at >= 0 {
		fraction = int64(len(mantissa) - at - 1)
		mantissa = mantissa[:at] + mantissa[at+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		*n = 0
		return nil
	}
	exponent := int64(0)
	if hasExponent {
		var err error
		exponent, err = strconv.ParseInt(exponentText, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: integer range", ErrType)
		}
	}
	if exponent < -int64(len(data)) || exponent > int64(len(data))+19 {
		return fmt.Errorf("%w: integer range", ErrType)
	}
	power := exponent - fraction
	if power >= 0 {
		if int64(len(digits))+power > 19 {
			return fmt.Errorf("%w: integer range", ErrType)
		}
		digits += strings.Repeat("0", int(power))
	} else {
		scale := -power
		if scale >= int64(len(digits)) {
			return fmt.Errorf("%w: fractional number", ErrType)
		}
		for _, digit := range digits[len(digits)-int(scale):] {
			if digit != '0' {
				return fmt.Errorf("%w: fractional number", ErrType)
			}
		}
		digits = digits[:len(digits)-int(scale)]
	}
	if negative {
		digits = "-" + digits
	}
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: integer range", ErrType)
	}
	*n = Int(value)
	return nil
}
