#!/usr/bin/env bash
# End-to-end Agora CLI checks in a persistent termctrl session.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SESSION="${AGORA_E2E_SESSION:-agora-e2e-$$}"
COLS="${AGORA_E2E_COLS:-100}"
ROWS="${AGORA_E2E_ROWS:-35}"
COMMAND_TIMEOUT_MS="${AGORA_E2E_COMMAND_TIMEOUT_MS:-10000}"
DELIBERATION_TIMEOUT_MS="${AGORA_E2E_DELIBERATION_TIMEOUT_MS:-300000}"
DRY_RUN="${AGORA_E2E_DRY_RUN:-1}"
MODEL="${AGORA_E2E_MODEL:-opencode/big-pickle}"

if ! command -v termctrl >/dev/null; then
	echo "termctrl not found in PATH" >&2
	exit 1
fi

if ! command -v go >/dev/null; then
	echo "go not found in PATH" >&2
	exit 1
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/agora-e2e.XXXXXX")"
BINARY="$TMP_DIR/agora"
TRANSCRIPT="$TMP_DIR/deliberation.jsonl"
CONFIG_HOME="$TMP_DIR/config"
DATA_HOME="$TMP_DIR/data"

cleanup() {
	termctrl stop "$SESSION" >/dev/null 2>&1 || true
	rm -rf "$TMP_DIR"
}
trap cleanup EXIT

show_failure() {
	termctrl show "$SESSION" >&2 || termctrl logs "$SESSION" >&2 || true
}

check_number=0
run_check() {
	local label="$1"
	local expected="$2"
	local timeout_ms="$3"
	local command marker out
	shift 3

	check_number=$((check_number + 1))
	marker="__AGORA_E2E_${BASHPID}_${check_number}_DONE__"
	printf -v command '%q ' "$@"

	termctrl send "$SESSION" \
		"text:printf '\033[2J\033[H'; $command; rc=\$?; printf '\n${marker}:%s\n' \"\$rc\"" \
		enter
	if ! termctrl wait "$SESSION" "$marker" --timeout "$timeout_ms"; then
		echo "FAIL: $label timed out after ${timeout_ms}ms" >&2
		show_failure
		exit 1
	fi

	out="$(termctrl show "$SESSION")"
	if [[ "$out" != *"${marker}:0"* ]]; then
		echo "FAIL: $label exited unsuccessfully" >&2
		echo "$out" >&2
		exit 1
	fi
	if [[ "$out" != *"$expected"* ]]; then
		echo "FAIL: $label did not show: $expected" >&2
		echo "$out" >&2
		exit 1
	fi
	echo "OK: $label"
}

cd "$ROOT"
termctrl stop "$SESSION" >/dev/null 2>&1 || true

echo "=== agora e2e (termctrl) ==="
echo "building binary..."
# VCS metadata is immaterial to the smoke test and can fail in managed worktrees.
go build -buildvcs=false -o "$BINARY" -trimpath "-ldflags=-s -w" ./cmd/agora

echo "starting termctrl session..."
termctrl start \
	--cols "$COLS" \
	--rows "$ROWS" \
	--cwd "$ROOT" \
	"$SESSION" \
	-- env TERM=xterm-256color bash --noprofile --norc -c \
	"stty -echo; exec bash --noprofile --norc"

run_check "--help" "Agora" "$COMMAND_TIMEOUT_MS" "$BINARY" --help
run_check "prime" "Agora Prime" "$COMMAND_TIMEOUT_MS" "$BINARY" prime
run_check \
	"validate" \
	"Configuration Valid" \
	"$COMMAND_TIMEOUT_MS" \
	"$BINARY" validate examples/quick-sanity-check.yaml
run_check \
	"list" \
	"Managed Transcripts" \
	"$COMMAND_TIMEOUT_MS" \
	env XDG_CONFIG_HOME="$CONFIG_HOME" XDG_DATA_HOME="$DATA_HOME" "$BINARY" list

topic="e2e test"
label="dry-run deliberation"
run_args=(run --auto quick --topic "$topic" --model "$MODEL" --dry-run --yes --output "$TRANSCRIPT")
if [[ "$DRY_RUN" == "0" ]]; then
	topic="is 2+2 4"
	label="live deliberation"
	run_args=(
		run --auto quick --topic "$topic" --model "$MODEL"
		--time 120 --max-turns 2 --yes --output "$TRANSCRIPT"
	)
fi
run_check \
	"$label" \
	"Deliberation complete" \
	"$DELIBERATION_TIMEOUT_MS" \
	"$BINARY" "${run_args[@]}"

echo ""
echo "E2E passed"
