package agents

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"configflow/internal/deploy/contentsync"
	"configflow/internal/deploy/pushdiff"
)

func contentPageQuery(version, path, change, offset, limit, query string) (contentsync.EntriesQuery, error) {
	result := contentsync.EntriesQuery{Version: version, Path: path, Change: change, Query: pushdiff.SearchQuery(query)}
	number := func(raw string, fallback, low, high int) (int, error) {
		if raw == "" {
			return fallback, nil
		}
		for i := range raw {
			if raw[i] < '0' || raw[i] > '9' {
				return 0, fmt.Errorf("invalid page number")
			}
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || n < uint64(low) || n > uint64(high) {
			return 0, fmt.Errorf("page number out of range")
		}
		return int(n), nil
	}
	var err error
	result.Offset, err = number(offset, 0, 0, 10000000)
	if err != nil {
		return result, err
	}
	result.Limit, err = number(limit, pushdiff.PageSize, 1, pushdiff.MaxPageSize)
	if err != nil {
		return result, err
	}
	if version == "" || utf8.RuneCountInString(version) > 64 || path == "" || utf8.RuneCountInString(path) > 1024 || utf8.RuneCountInString(query) > pushdiff.MaxQueryChars {
		return result, fmt.Errorf("invalid page arguments")
	}
	switch change {
	case "added", "removed", "modified":
	default:
		return result, fmt.Errorf("invalid page change")
	}
	return result, nil
}
