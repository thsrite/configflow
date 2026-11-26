package subs

import (
	"encoding/base64"
	"errors"
	"unicode/utf8"
)

// decodeSubscriptionBase64 tolerates external subscription-source formatting:
// ignore non-alphabet ASCII bytes (including spaces/line breaks), ignore stray
// padding until it completes a 2- or 3-character final quantum, then stop and
// ignore the suffix. Non-ASCII input and an incomplete final quantum are invalid.
// This acceptance range is intentional user compatibility, not an exception or
// runtime emulation. The standard library performs all actual bit decoding.
func decodeSubscriptionBase64(source string) ([]byte, error) {
	for i := 0; i < len(source); i++ {
		if source[i] >= 128 {
			return nil, errSubscriptionBase64
		}
	}
	cleaned := make([]byte, 0, len(source))
	padding := 0
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c == '=' {
			remainder := len(cleaned) % 4
			if remainder >= 2 {
				padding++
				if remainder+padding >= 4 {
					for len(cleaned)%4 != 0 {
						cleaned = append(cleaned, '=')
					}
					return base64.StdEncoding.DecodeString(string(cleaned))
				}
			}
			continue
		}
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' {
			cleaned = append(cleaned, c)
			padding = 0
		}
	}
	return base64.StdEncoding.DecodeString(string(cleaned))
}

var errSubscriptionBase64 = errors.New("invalid subscription base64")

func decodeSubscriptionText(source string) (string, error) {
	data, err := decodeSubscriptionBase64(source)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("subscription text is not UTF-8")
	}
	return string(data), nil
}
