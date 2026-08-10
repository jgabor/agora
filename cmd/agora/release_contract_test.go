package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

var releaseHeading = regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func TestReleaseVersionMatchesChangelog(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate release contract test")
	}
	changelog, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if err := validateReleaseVersion(string(changelog), version); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseVersionRejectsChangelogMismatch(t *testing.T) {
	if err := validateReleaseVersion("## [0.4.4] - 2026-08-10\n", version); err == nil {
		t.Fatal("mismatched changelog version was accepted")
	}
}

func validateReleaseVersion(changelog, want string) error {
	match := releaseHeading.FindStringSubmatch(changelog)
	if match == nil {
		return fmt.Errorf("changelog has no dated release heading")
	}
	if match[1] != want {
		return fmt.Errorf("changelog version %q does not match CLI version %q", match[1], want)
	}
	return nil
}
