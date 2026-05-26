#!/usr/bin/env bash
# End-to-end Agora CLI checks via rmux (detached session + capture-pane).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SESSION="${AGORA_E2E_SESSION:-agora-e2e}"
COLS="${AGORA_E2E_COLS:-100}"
ROWS="${AGORA_E2E_ROWS:-35}"
WAIT="${AGORA_E2E_WAIT:-2}"
DRY_RUN="${AGORA_E2E_DRY_RUN:-1}"

cleanup() {
	rmux kill-session -t "$SESSION" 2>/dev/null || true
	rm -f /tmp/agora-e2e
}
trap cleanup EXIT

cd "$ROOT"

if ! command -v rmux >/dev/null; then
	echo "rmux not found in PATH" >&2
	exit 1
fi

if ! command -v go >/dev/null; then
	echo "go not found in PATH" >&2
	exit 1
fi

cleanup

echo "=== agora e2e (rmux) ==="
echo "building binary..."
go build -o /tmp/agora-e2e -trimpath "-ldflags=-s -w" ./cmd/agora

echo "starting rmux session..."
rmux new-session -d -s "$SESSION" -c "$ROOT" -x "$COLS" -y "$ROWS"

# 1. --help
rmux send-keys -t "$SESSION" "/tmp/agora-e2e --help" Enter
sleep "$WAIT"
out="$(rmux capture-pane -t "$SESSION" -p -S -20)"
if ! grep -q "Agora" <<<"$out"; then
	echo "FAIL: --help" >&2
	echo "$out" >&2
	exit 1
fi
echo "OK: --help"

# 2. prime
rmux send-keys -t "$SESSION" "clear; /tmp/agora-e2e prime" Enter
sleep "$WAIT"
out="$(rmux capture-pane -t "$SESSION" -p -S -20)"
if ! grep -q "Agora Prime" <<<"$out"; then
	echo "FAIL: prime" >&2
	echo "$out" >&2
	exit 1
fi
echo "OK: prime"

# 3. validate example config
rmux send-keys -t "$SESSION" "clear; /tmp/agora-e2e validate examples/quick-sanity-check.yaml" Enter
sleep "$WAIT"
out="$(rmux capture-pane -t "$SESSION" -p -S -20)"
if ! grep -q "Configuration Valid" <<<"$out"; then
	echo "FAIL: validate" >&2
	echo "$out" >&2
	exit 1
fi
echo "OK: validate"

# 4. list (succeeds with any output — store may or may not have transcripts)
rmux send-keys -t "$SESSION" "clear; /tmp/agora-e2e list" Enter
sleep "$WAIT"
out="$(rmux capture-pane -t "$SESSION" -p -S -15)"
if ! grep -q "Managed Transcripts" <<<"$out"; then
	echo "FAIL: list" >&2
	echo "$out" >&2
	exit 1
fi
echo "OK: list"

# 5. deliberation (AGORA_E2E_DRY_RUN=0 for real API call, needs keys)
BASE_TOPIC="e2e test"
EXTRA="--dry-run"
LABEL="dry-run"
if [[ "$DRY_RUN" == "0" ]]; then
	# trivial topic for fast live responses; give extra time for real API calls
	BASE_TOPIC="is 2+2 4"
	EXTRA="--time 120 --max-turns 2"
	LABEL="live"
fi
rmux send-keys -t "$SESSION" "clear; /tmp/agora-e2e run --auto quick --topic '$BASE_TOPIC' $EXTRA --yes" Enter
# Poll every 5s up to 120s — live API calls can take 60+ seconds
elapsed=0
while true; do
	sleep 5
	elapsed=$((elapsed + 5))
	out="$(rmux capture-pane -t "$SESSION" -p -S -25)"
	if grep -q "Deliberation complete\|Halted by" <<<"$out"; then
		echo "OK: $LABEL deliberation (${elapsed}s)"
		break
	fi
	if grep -q "Status.*Aborted" <<<"$out"; then
		echo "FAIL: $LABEL deliberation aborted" >&2
		echo "$out" >&2
		exit 1
	fi
	if (( elapsed >= 300 )); then
		echo "FAIL: $LABEL deliberation timed out (300s)" >&2
		echo "$out" >&2
		exit 1
	fi
done

echo ""
echo "E2E passed"