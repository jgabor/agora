package llmutil

import (
	"encoding/json"
	"regexp"
	"strings"
)

var fencePattern = regexp.MustCompile("(?s)```(?:ya?ml|json)?\n(.*?)\n```")

func StripCodeFences(s string) string {
	m := fencePattern.FindStringSubmatch(s)
	if m == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(m[1])
}

func ExtractJSON(s string, v any) error {
	cleaned := StripCodeFences(s)

	// Try 1: parse the whole string as-is
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.UseNumber()
	if err := dec.Decode(v); err == nil {
		return nil
	}

	// Try 2: find first '{' or '[' and parse from there
	if start := strings.IndexAny(cleaned, "{["); start >= 0 {
		dec = json.NewDecoder(strings.NewReader(cleaned[start:]))
		dec.UseNumber()
		if err := dec.Decode(v); err == nil {
			return nil
		}
	}

	// Try 3: strip stray backticks (invalid JSON tokens that LLMs often emit)
	stripped := strings.ReplaceAll(cleaned, "`", "")
	if stripped != cleaned {
		dec = json.NewDecoder(strings.NewReader(stripped))
		dec.UseNumber()
		return dec.Decode(v)
	}

	// Original attempt for a consistent error
	dec = json.NewDecoder(strings.NewReader(cleaned))
	dec.UseNumber()
	return dec.Decode(v)
}
