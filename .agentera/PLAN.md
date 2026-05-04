# Plan: Merge Go Port to Main, Remove Python

<!-- Level: light · Created: 2026-05-04 · Status: active -->

## What
Merge the go-port branch into main, then delete all Python source files (kumbaja/, tests/, loop.py, LOOP.md, pyproject.toml, uv.lock, .python-version, participants.jsonl, transcript.jsonl). Update README.md to reflect Go as the sole implementation. Add a Decision 2 recording the migration.

## Why
The Go port is complete and verified (7 commits, 43 tests). Keeping both implementations creates confusion about which is authoritative and doubles maintenance surface.

## Constraints
- Go build and tests must still pass after merge
- .agentera/ artifacts, example-config.yaml, and .gitignore must survive
- The Go port's behavior must not change
- DECISIONS.md must record the migration with a new firm entry

## Acceptance Criteria
▸ GIVEN the merge is complete WHEN `git log --oneline main` is checked THEN it contains all go-port commits
▸ GIVEN the Python files are removed WHEN `ls kumbaja/` is attempted THEN the directory does not exist
▸ GIVEN the Go project remains WHEN `go build ./... && go test ./...` is run THEN all build and tests pass
▸ GIVEN the README is updated WHEN it is read THEN it shows Go installation and usage instructions (not Python uv commands)
▸ GIVEN the migration is complete WHEN DECISIONS.md is checked THEN it contains a Decision 2 recording the language migration
