package main

import "agent/internal/logx"

// redactURLForLog returns a URL suitable for logs without userinfo or query credentials.
func redactURLForLog(raw string) string {
	return logx.RedactURL(raw)
}

func safeURLFailure(action, rawURL string, cause error) error {
	return logx.SafeURLFailure(action, rawURL, cause)
}
