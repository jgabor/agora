#!/usr/bin/env bash
# Provider-free Mage entrypoint checks for scripts/eval-cli-discovery.sh.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/eval-cli-discovery.sh
source "$ROOT/scripts/eval-cli-discovery.sh"

checks=0
TEST_TMP=""

pass() {
	printf 'ok %s\n' "$1"
	checks=$((checks + 1))
}

fail() {
	printf 'not ok %s\n' "$1" >&2
	exit 1
}

check() {
	local name="$1"
	shift

	if "$@"; then
		pass "$name"
	else
		fail "$name"
	fi
}

write_fake_opencode() {
	local path="$1"
	local args_file="$2"
	local invoked_file="$3"
	local auth_file="$4"
	local args_file_q
	local invoked_file_q
	local auth_file_q

	printf -v args_file_q '%q' "$args_file"
	printf -v invoked_file_q '%q' "$invoked_file"
	printf -v auth_file_q '%q' "$auth_file"
	cat >"$path" <<EOF
#!/usr/bin/env bash
set -euo pipefail

if [[ "\${1:-}" == "--version" ]]; then
	printf 'fake-opencode 1.0\\n'
	exit 0
fi

: >$invoked_file_q
printf '%s\\0' "\$@" >$args_file_q
jq -e 'keys == ["opencode"] and .opencode == {type: "api", key: "mage-forward-token"}' \\
	<<<"\${OPENCODE_AUTH_CONTENT:?}" >/dev/null
: >$auth_file_q
cat >"\$AGORA_EVALUATOR_TRANSCRIPT" <<'TRANSCRIPT'
{"turn":-2,"agent_id":"moderator","transcript":{"cast":[],"config":{"research":false,"agents":[]}},"evidence":{"source_references":[]}}
TRANSCRIPT
jq -nc --arg model "\$AGORA_EVALUATOR_MODEL" '{turn: 0, agent_id: "agent", model: \$model, tokens: {total: 0, input: 0, output: 0, reasoning: 0, cache: {read: 0, write: 0}}, cost: 0}' >>"\$AGORA_EVALUATOR_TRANSCRIPT"
cat <<'EVENTS'
{"type":"tool_use","timestamp":1,"part":{"id":"mage-forwarding","callID":"call-mage","tool":"bash","state":{"status":"completed","input":{"command":"agora run --auto quick --research --yes --topic mage-forwarding"},"metadata":{"exit":0},"output":"completed"}}}
{"type":"step_finish","timestamp":2,"part":{"tokens":{"total":0,"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},"cost":0}}
EVENTS
EOF
	chmod 700 "$path"
}

write_fake_timeout() {
	local path="$1"
	local args_file="$2"
	local args_file_q

	printf -v args_file_q '%q' "$args_file"
	cat >"$path" <<EOF
#!/usr/bin/env bash
set -euo pipefail

printf '%s\\0' "\$@" >$args_file_q
shift
exec "\$@"
EOF
	chmod 700 "$path"
}

build_mage() {
	go -C "$ROOT" build -buildvcs=false -o "$TEST_TMP/mage" github.com/magefile/mage
}

mage_is_repo_pinned() {
	local version

	version="$(go -C "$ROOT" list -m -f '{{.Version}}' github.com/magefile/mage)"
	"$TEST_TMP/mage" --version | grep -Fq "Mage Build Tool $version"
}

listing_and_help_are_safe() {
	local stdout="$TEST_TMP/list.stdout"
	local stderr="$TEST_TMP/list.stderr"

	rm -f -- "$TEST_TMP/opencode-invoked"
	MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" -l >"$stdout" 2>"$stderr" \
		&& grep -Fq 'eval:cliDiscovery' "$stdout" \
		&& grep -Fq 'eval:cliDiscoveryHelp' "$stdout" \
		&& grep -Fq 'eval:cliDiscoveryOfflineSelfTest' "$stdout" \
		&& grep -Fq 'eval:cliDiscoverySelfTest' "$stdout" \
		&& test ! -e "$TEST_TMP/opencode-invoked"
}

target_help_is_safe() {
	local stdout="$TEST_TMP/help.stdout"
	local stderr="$TEST_TMP/help.stderr"

	rm -f -- "$TEST_TMP/opencode-invoked"
	MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" -h eval:cliDiscovery >"$stdout" 2>"$stderr" \
		&& grep -Fq '<output>' "$stdout" \
		&& test ! -e "$TEST_TMP/opencode-invoked" \
		&& MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" eval:cliDiscoveryHelp >"$stdout" 2>"$stderr" \
		&& grep -Fq 'scripts/eval-cli-discovery.sh --output DIR' "$stdout" \
		&& test ! -e "$TEST_TMP/opencode-invoked"
}

invalid_arguments_are_safe() {
	local stdout="$TEST_TMP/invalid.stdout"
	local stderr="$TEST_TMP/invalid.stderr"

	rm -f -- "$TEST_TMP/opencode-invoked"
	if MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" eval:cliDiscovery >"$stdout" 2>"$stderr"; then
		return 1
	fi
	grep -Fq 'not enough arguments for target "Eval:CliDiscovery"' "$stderr" \
		&& test ! -e "$TEST_TMP/opencode-invoked" \
		&& ! test -e "$TEST_TMP/missing-output" \
		|| return 1

	if MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" eval:cliDiscovery "$TEST_TMP/invalid-output" -quiet=not-a-bool >"$stdout" 2>"$stderr"; then
		return 1
	fi
	grep -Fq "can't convert option \"quiet\" value \"not-a-bool\" to bool" "$stderr" \
		&& test ! -e "$TEST_TMP/opencode-invoked" \
		&& ! test -e "$TEST_TMP/invalid-output" \
		|| return 1

	if MAGEFILE_CACHE="$TEST_TMP/mage-cache" "$TEST_TMP/mage" eval:cliDiscovery '' >"$stdout" 2>"$stderr"; then
		return 1
	fi
	grep -Fq 'output is required' "$stderr" \
		&& test ! -e "$TEST_TMP/opencode-invoked"
}

forwarded_trial_is_exact() {
	# shellcheck disable=SC2016 # Intentional literal exercises Mage's former os.Expand behavior.
	local literal_sentinel='$MAGE_FORWARD_SENTINEL'
	local expanded_sentinel='EXPANDED_VALUE_MUST_NOT_APPEAR'
	local decoration=" $literal_sentinel \"double quote\" 'single quote'"
	local output="$TEST_TMP/output$decoration"
	local expanded_output="$TEST_TMP/output ${expanded_sentinel} \"double quote\" 'single quote'"
	local auth="$TEST_TMP/auth$decoration.json"
	local model="opencode/test-model$decoration"
	local timeout_value="37s$decoration"
	local stdout="$TEST_TMP/trial.stdout"
	local stderr="$TEST_TMP/trial.stderr"
	local expected="$TEST_TMP/trial.expected"
	local -a opencode_args
	local -a timeout_args

	jq -n '{opencode: {type: "api", key: "mage-forward-token"}, unrelated: {type: "api", key: "unrelated-token"}}' >"$auth"
	rm -f -- "$TEST_TMP/opencode-invoked"
	PATH="$TEST_TMP/tools:$PATH" \
		BASH_ENV=/dev/null \
		MAGE_FORWARD_SENTINEL="$expanded_sentinel" \
		MAGEFILE_CACHE="$TEST_TMP/mage-cache" \
		AGORA_EVALUATOR_OPENCODE_BIN="$TEST_TMP/fake-opencode" \
		"$TEST_TMP/mage" eval:cliDiscovery "$output" \
			-model="$model" \
			-authFile="$auth" \
			-timeout="$timeout_value" \
			-quiet >"$stdout" 2>"$stderr" || return 1
	printf '%s\n' "$output/result.json" >"$expected"
	cmp "$expected" "$stdout" \
		&& test ! -s "$stderr" \
		&& test -f "$output/result.json" \
		&& test ! -e "$expanded_output" \
		&& test -e "$TEST_TMP/opencode-invoked" \
		&& test -e "$TEST_TMP/auth-accepted" \
		&& ! grep -RFq -- 'mage-forward-token' "$stdout" "$stderr" "$output" \
		|| return 1

	mapfile -d '' -t opencode_args <"$TEST_TMP/opencode.args"
	((${#opencode_args[@]} == 13)) \
		&& [[ "${opencode_args[0]}" == run ]] \
		&& [[ "${opencode_args[1]}" == --pure ]] \
		&& [[ "${opencode_args[2]}" == --format ]] \
		&& [[ "${opencode_args[3]}" == json ]] \
		&& [[ "${opencode_args[4]}" == --model ]] \
		&& [[ "${opencode_args[5]}" == "$model" ]] \
		&& [[ "${opencode_args[6]}" == --agent ]] \
		&& [[ "${opencode_args[7]}" == "$EVALUATOR_AGENT" ]] \
		&& [[ "${opencode_args[8]}" == --title ]] \
		&& [[ "${opencode_args[9]}" == 'Agora CLI discovery evaluation' ]] \
		&& [[ "${opencode_args[10]}" == --dir ]] \
		&& [[ "${opencode_args[11]}" == */work ]] \
		&& [[ "${opencode_args[12]}" == "$PROMPT" ]] \
		&& ! grep -aFq -- "$expanded_sentinel" "$TEST_TMP/opencode.args" \
		|| return 1

	mapfile -d '' -t timeout_args <"$TEST_TMP/timeout.args"
	((${#timeout_args[@]} == 15)) \
		&& [[ "${timeout_args[0]}" == "$timeout_value" ]] \
		&& [[ "${timeout_args[1]}" == "$TEST_TMP/fake-opencode" ]] \
		&& ! grep -aFq -- "$expanded_sentinel" "$TEST_TMP/timeout.args"
}

failed_trial_redacts_mage_error() {
	local output="$TEST_TMP/failure output 'private'"
	local auth="$TEST_TMP/failure auth \"private\".json"
	local model='opencode/failure-private-model'
	local stdout="$TEST_TMP/failure.stdout"
	local stderr="$TEST_TMP/failure.stderr"
	local status

	jq -n '{opencode: {type: "api", key: "mage-failure-token"}}' >"$auth"
	rm -f -- "$TEST_TMP/opencode-invoked"
	set +e
	MAGEFILE_CACHE="$TEST_TMP/mage-cache" \
		AGORA_EVALUATOR_OPENCODE_BIN="$TEST_TMP/fake-opencode" \
		"$TEST_TMP/mage" eval:cliDiscovery "$output" \
			-model="$model" \
			-authFile="$auth" \
			-timeout=not-a-duration \
			-quiet >"$stdout" 2>"$stderr"
	status=$?
	set -e

	[[ "$status" == 125 ]] \
		&& test ! -s "$stdout" \
		&& test ! -e "$TEST_TMP/opencode-invoked" \
		&& grep -Fq 'invalid time interval' "$output/opencode.stderr" \
		&& grep -Fq 'evaluator failed with exit code 125' "$stderr" \
		&& ! grep -RFq -- "$auth" "$stdout" "$stderr" "$output" \
		&& ! grep -RFq -- 'mage-failure-token' "$stdout" "$stderr" "$output" \
		&& ! grep -Fq -- "$model" "$stdout" "$stderr" \
		&& ! grep -Fq -- "$output" "$stdout" "$stderr" \
		&& ! grep -Fq -- '--auth-file' "$stdout" "$stderr"
}

main() {
	TEST_TMP="$(mktemp -d "${TMPDIR:-/tmp}/agora-cli-evaluator-mage-test.XXXXXX")"
	trap 'rm -rf -- "$TEST_TMP"' EXIT
	mkdir -p -- "$TEST_TMP/tools"
	write_fake_opencode "$TEST_TMP/fake-opencode" "$TEST_TMP/opencode.args" "$TEST_TMP/opencode-invoked" "$TEST_TMP/auth-accepted"
	write_fake_timeout "$TEST_TMP/tools/timeout" "$TEST_TMP/timeout.args"
	build_mage

	check 'Mage binary matches the repository-pinned version' mage_is_repo_pinned
	check 'Mage listing exposes live, help, offline, and full evaluator targets without evaluation' listing_and_help_are_safe
	check 'Mage evaluator help does not start evaluation' target_help_is_safe
	check 'Mage rejects missing, invalid, and empty live target arguments before evaluation' invalid_arguments_are_safe
	check 'Mage preserves evaluator failure status without reproducing sensitive argv' failed_trial_redacts_mage_error
	check 'Mage forwards adversarial output, model, auth, timeout, and quiet values literally' forwarded_trial_is_exact
	printf 'provider-free Mage evaluator self-test passed (%d checks)\n' "$checks"
}

main "$@"
