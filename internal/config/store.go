package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// TranscriptOutputPath returns the managed transcript path for a topic.
func TranscriptOutputPath(topic string, settings Settings, now time.Time) (string, error) {
	dir := settings.DefaultOutputDir
	if dir == "" {
		var err error
		dir, err = TranscriptStoreDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", now.Format("20060102-150405"), TopicSlug(topic))), nil
}

// TopicSlug normalizes a topic for transcript filenames.
func TopicSlug(topic string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(topic) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r) || r == '-':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		if b.Len() >= 50 {
			break
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "deliberation"
	}
	return slug
}
