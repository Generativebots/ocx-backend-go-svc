package protocol

import (
	"bytes"
	"errors"
)

var errNotJSON = errors.New("payload is not JSON")

// findJSONStart scans payload for the start of a JSON object or array.
// HTTP payloads often have headers before the JSON body, so we skip past them.
func findJSONStart(data []byte) int {
	// Fast path: payload starts with JSON
	if len(data) > 0 && (data[0] == '{' || data[0] == '[') {
		return 0
	}

	// Look for HTTP body separator (double CRLF)
	if idx := bytes.Index(data, []byte("\r\n\r\n")); idx >= 0 {
		bodyStart := idx + 4
		if bodyStart < len(data) && (data[bodyStart] == '{' || data[bodyStart] == '[') {
			return bodyStart
		}
	}

	// Look for double LF (non-standard HTTP)
	if idx := bytes.Index(data, []byte("\n\n")); idx >= 0 {
		bodyStart := idx + 2
		if bodyStart < len(data) && (data[bodyStart] == '{' || data[bodyStart] == '[') {
			return bodyStart
		}
	}

	// Scan for first '{' or '['
	for i, b := range data {
		if b == '{' || b == '[' {
			return i
		}
	}

	return -1
}
