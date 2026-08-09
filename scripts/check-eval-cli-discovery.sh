#!/usr/bin/env bash
# CI-safe checks for the CLI-discovery evaluator.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SHELLCHECK_BIN="${AGORA_EVALUATOR_SHELLCHECK_BIN:-shellcheck}"
SHELLCHECK_VERSION="0.11.0"
SCRIPTS=(
	"$ROOT/scripts/eval-cli-discovery.sh"
	"$ROOT/scripts/eval-cli-discovery-test.sh"
	"$ROOT/scripts/eval-cli-discovery-mage-test.sh"
	"$ROOT/scripts/check-eval-cli-discovery.sh"
)
cd "$ROOT"

# shellcheck source=scripts/eval-cli-discovery.sh
source "$ROOT/scripts/eval-cli-discovery.sh"

require_shellcheck() {
	local version

	command -v -- "$SHELLCHECK_BIN" >/dev/null 2>&1 \
		|| {
			printf 'error: ShellCheck %s is required at %s\n' "$SHELLCHECK_VERSION" "$SHELLCHECK_BIN" >&2
			return 2
		}
	version="$("$SHELLCHECK_BIN" --version | awk '$1 == "version:" { print $2; exit }')"
	[[ "$version" == "$SHELLCHECK_VERSION" ]] \
		|| {
			printf 'error: expected ShellCheck %s at %s, got %s\n' "$SHELLCHECK_VERSION" "$SHELLCHECK_BIN" "${version:-unknown}" >&2
			return 2
		}
}

result_root_is_ignored() (
	local probe_id="git-status-${BASHPID}-${RANDOM}"
	local output="$ROOT/$EVALUATOR_RESULT_ROOT_RELATIVE/$probe_id"
	local output_relative="$EVALUATOR_RESULT_ROOT_RELATIVE/$probe_id"
	local sibling="$ROOT/.agora-evaluator/$probe_id-unrelated"
	local sibling_relative=".agora-evaluator/$probe_id-unrelated"
	local status

	trap 'rm -rf -- "$output" "$sibling"' EXIT
	mkdir -p -- "$output/temporary-state/opencode"
	: >"$output/result.json"
	: >"$output/events.jsonl"
	: >"$output/agora-transcript.jsonl"
	: >"$output/opencode.stderr"
	: >"$output/agora-invocations.log"
	: >"$output/shell-environment.log"
	: >"$output/boundary.json"
	: >"$output/temporary-state/config.yaml"
	: >"$output/temporary-state/opencode/auth.json"
	: >"$sibling"

	git -C "$ROOT" check-ignore -q -- "$output_relative/result.json"
	status="$(git -C "$ROOT" status --porcelain --untracked-files=all)"
	[[ "$status" != *"$output_relative"* ]]
	[[ "$status" == *"?? $sibling_relative"* ]]
)

require_shellcheck
bash -n "${SCRIPTS[@]}"
"$SHELLCHECK_BIN" -x "${SCRIPTS[@]}"
result_root_is_ignored
printf 'ok evaluator result root ignores outputs without hiding siblings\n'
"$ROOT/scripts/eval-cli-discovery-mage-test.sh"
"$ROOT/scripts/eval-cli-discovery.sh" --analysis-self-test
