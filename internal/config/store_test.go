package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTranscriptOutputPathUsesDefaultStore(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	now := time.Date(2026, 5, 4, 14, 30, 22, 0, time.UTC)

	path, err := TranscriptOutputPath("My Topic", Settings{}, now)
	if err != nil {
		t.Fatalf("TranscriptOutputPath: %v", err)
	}

	want := filepath.Join(dataHome, "agora", "transcripts", "20260504-143022-my-topic.jsonl")
	if path != want {
		t.Fatalf("path: got %q, want %q", path, want)
	}
}

func TestTranscriptOutputPathUsesDefaultOutputDir(t *testing.T) {
	now := time.Date(2026, 5, 4, 14, 30, 22, 0, time.UTC)

	path, err := TranscriptOutputPath("Test", Settings{DefaultOutputDir: "/tmp/agora"}, now)
	if err != nil {
		t.Fatalf("TranscriptOutputPath: %v", err)
	}

	want := filepath.Join("/tmp/agora", "20260504-143022-test.jsonl")
	if path != want {
		t.Fatalf("path: got %q, want %q", path, want)
	}
}

func TestTopicSlug(t *testing.T) {
	got := TopicSlug(" My, Very Long Topic! With   Spaces ")
	want := "my-very-long-topic-with-spaces"
	if got != want {
		t.Fatalf("slug: got %q, want %q", got, want)
	}
}
