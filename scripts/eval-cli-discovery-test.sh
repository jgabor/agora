#!/usr/bin/env bash
# Provider-free regression checks for scripts/eval-cli-discovery.sh.
set -euo pipefail

TEST_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source "$TEST_ROOT/scripts/eval-cli-discovery.sh"

checks=0
TEST_TMP=""
HOST_OPENCODE=""
QUALIFYING_COMMAND='agora run --auto quick --research --yes --topic "a quick debate between grumpy old people on the latest weather report"'

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

expect_rejected() {
	local name="$1"
	shift
	if "$@" >/dev/null 2>&1; then
		fail "$name (unexpectedly accepted)"
	else
		pass "$name"
	fi
}

assert_equal() {
	local want="$1"
	local got="$2"
	[[ "$want" == "$got" ]]
}

json_assert() {
	local expression="$1"
	local file="$2"
	jq -e "$expression" "$file" >/dev/null
}

transcript_uses_model() {
	local model="$1"
	local transcript="$2"
	jq -e -s --arg model "$model" '[.[] | .transcript? | select(type == "object") | (.cast // [])[] | .provider_model | select(type == "string")] as $models | ($models | length > 0 and all(. == $model))' "$transcript" >/dev/null
}

permission_config_is_exact() {
	local config="$1"
	local shell_gate="$2"

	jq -e --arg agent "$EVALUATOR_AGENT" --arg shell "$shell_gate" '
		.shell == $shell
		and .permission["*"] == "deny"
		and .permission.bash == "allow"
		and .agent[$agent].mode == "primary"
		and ((.agent[$agent].permission // {}) == {})
	' "$config" >/dev/null
}

prompt_preserves_discovery() {
	local expected_prompt="Run \`agora\` and initiate a quick debate between grumpy old people on the latest weather report."

	[[ "$PROMPT" == "$expected_prompt" ]] \
		&& [[ "$PROMPT" != *'agora run'* ]] \
		&& [[ "$PROMPT" != *'--auto'* ]] \
		&& [[ "$PROMPT" != *'--research'* ]] \
		&& [[ "$PROMPT" != *'--yes'* ]] \
		&& [[ "$PROMPT" != *'--topic'* ]] \
		&& [[ "$PROMPT" != *'--dry-run'* ]]
}

tool_status_is() {
	local events="$1"
	local status="$2"

	jq -e -s --arg status "$status" 'any(.[]; .type == "tool_use" and .part.state.status == $status)' "$events" >/dev/null
}

tool_exit_is() {
	local events="$1"
	local exit_code="$2"

	jq -e -s --argjson exit_code "$exit_code" 'any(.[]; .type == "tool_use" and .part.state.metadata.exit == $exit_code)' "$events" >/dev/null
}

wait_for_file() {
	local path="$1"
	local attempts="${2:-100}"
	local attempt

	for ((attempt=0; attempt<attempts; attempt++)); do
		[[ -s "$path" ]] && return 0
		sleep 0.05
	done
	return 1
}

write_fake_opencode() {
	local path="$1"
	cat >"$path" <<'FAKE_OPENCODE'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--version" ]]; then
	printf 'fake-opencode 1.0\n'
	exit 0
fi

real="${AGORA_EVALUATOR_REAL:?}"
[[ -x "$real" ]]
wrapper="$(command -v agora)"
real_version="$("$real" --version)"
auth_present=false
[[ -n "${OPENCODE_AUTH_CONTENT:-}" ]] && auth_present=true
auth_provider=""
auth_provider_count=0
if [[ "$auth_present" == true ]]; then
	auth_provider="$(jq -r 'keys | if length == 1 then .[0] else "" end' <<<"$OPENCODE_AUTH_CONTENT")"
	auth_provider_count="$(jq -r 'keys | length' <<<"$OPENCODE_AUTH_CONTENT")"
fi

agora --help >/dev/null
agora run --auto quick --research --yes --topic "a quick debate between grumpy old people on the latest weather report" >/dev/null

jq -nc \
	--arg home "$HOME" \
	--arg config_home "$XDG_CONFIG_HOME" \
	--arg data_home "$XDG_DATA_HOME" \
	--arg state_home "$XDG_STATE_HOME" \
	--arg cache_home "$XDG_CACHE_HOME" \
	--arg config_dir "$OPENCODE_CONFIG_DIR" \
	--arg path "$PATH" \
	--arg wrapper "$wrapper" \
	--arg real "$real" \
	--arg real_version "$real_version" \
	--arg config "$OPENCODE_CONFIG_CONTENT" \
	--arg args "$*" \
	--arg auth_provider "$auth_provider" \
	--argjson auth_present "$auth_present" \
	--argjson auth_provider_count "$auth_provider_count" \
	'{home: $home, config_home: $config_home, data_home: $data_home, state_home: $state_home, cache_home: $cache_home, config_dir: $config_dir, path: $path, wrapper: $wrapper, real: $real, real_version: $real_version, config: $config, args: $args, auth_present: $auth_present, auth_provider: $auth_provider, auth_provider_count: $auth_provider_count}'
FAKE_OPENCODE
	chmod 700 "$path"
}

write_leaf_leaking_opencode() {
	local path="$1"
	cat >"$path" <<'FAKE_OPENCODE'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--version" ]]; then
	printf 'fake-opencode 1.0\n'
	exit 0
fi

jq -r 'to_entries[0].value.key' <<<"${OPENCODE_AUTH_CONTENT:?}"
FAKE_OPENCODE
	chmod 700 "$path"
}

write_failing_opencode() {
	local path="$1"
	local status="$2"
	cat >"$path" <<FAKE_OPENCODE
#!/usr/bin/env bash
set -euo pipefail

if [[ "\${1:-}" == "--version" ]]; then
	printf 'fake-opencode 1.0\\n'
	exit 0
fi

printf 'fake outer failure\\n'
exit $status
FAKE_OPENCODE
	chmod 700 "$path"
}

write_interrupting_opencode() {
	local path="$1"
	local record_dir="$2"
	local record_dir_q

	printf -v record_dir_q '%q' "$record_dir"
	cat >"$path" <<FAKE_OPENCODE
#!/usr/bin/env bash
set -euo pipefail

if [[ "\${1:-}" == "--version" ]]; then
	printf 'fake-opencode 1.0\\n'
	exit 0
fi

record_dir=$record_dir_q
tmp_root="\$(dirname -- "\$(dirname -- "\$AGORA_EVALUATOR_REAL")")"
secret="\$(jq -r 'to_entries[0].value.key' <<<"\${OPENCODE_AUTH_CONTENT:?}")"
printf '%s\\n' "\$tmp_root" >"\$record_dir/temp-root"
(
	trap '' HUP INT TERM
	while :; do
		printf '%s\\n' "\$secret" >>"\$AGORA_EVALUATOR_INVOCATIONS"
		printf '%s\\n' "\$secret" >"\$tmp_root/credential-residue"
		sleep 0.01
	done
) &
child_pid=\$!
printf '%s\\n' "\$child_pid" >"\$record_dir/child.pid"
printf 'ready\\n' >"\$record_dir/ready"
trap '' HUP INT TERM
wait "\$child_pid"
FAKE_OPENCODE
	chmod 700 "$path"
}

write_recording_real() {
	local path="$1"
	cat >"$path" <<'RECORDING_REAL'
#!/usr/bin/env bash
set -euo pipefail
: "${REAL_ARGS_LOG:?}"
if [[ -n "${OPENCODE_AUTH_CONTENT:-}" ]]; then
	printf 'auth=present\n' >>"$REAL_ARGS_LOG"
else
	printf 'auth=absent\n' >>"$REAL_ARGS_LOG"
fi
printf '%q ' "$@" >>"$REAL_ARGS_LOG"
printf '\n' >>"$REAL_ARGS_LOG"
RECORDING_REAL
	chmod 700 "$path"
}

write_permission_provider() {
	local path="$1"
	cat >"$path" <<'PYTHON'
import http.server
import json
import sys

port_file, command = sys.argv[1:]
request_count = 0


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_POST(self):
        global request_count
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        request_count += 1
        if request_count == 1:
            chunks = [
                {
                    "id": "permission-probe-1",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "mock",
                    "choices": [
                        {
                            "index": 0,
                            "delta": {
                                "role": "assistant",
                                "tool_calls": [
                                    {
                                        "index": 0,
                                        "id": "call_1",
                                        "type": "function",
                                        "function": {
                                            "name": "bash",
                                            "arguments": json.dumps(
                                                {"command": command, "description": "permission probe"}
                                            ),
                                        },
                                    }
                                ],
                            },
                            "finish_reason": None,
                        }
                    ],
                },
                {
                    "id": "permission-probe-1",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "mock",
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}],
                },
            ]
        else:
            chunks = [
                {
                    "id": "permission-probe-2",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "mock",
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"role": "assistant", "content": "done"},
                            "finish_reason": None,
                        }
                    ],
                },
                {
                    "id": "permission-probe-2",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "mock",
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                },
            ]

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for chunk in chunks:
            self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(port_file, "w", encoding="utf-8") as handle:
    handle.write(str(server.server_port))
server.serve_forever()
PYTHON
	chmod 700 "$path"
}

write_process_supervisor() {
	local path="$1"

	cat >"$path" <<'PYTHON'
import signal
import subprocess
import sys

pid_file, status_file, *command = sys.argv[1:]


def reset_signals():
    signal.signal(signal.SIGINT, signal.SIG_DFL)
    signal.signal(signal.SIGTERM, signal.SIG_DFL)


process = subprocess.Popen(command, preexec_fn=reset_signals)
with open(pid_file, "w", encoding="utf-8") as handle:
    handle.write(str(process.pid))
status = process.wait()
if status < 0:
    status = 128 - status
with open(status_file, "w", encoding="utf-8") as handle:
    handle.write(str(status))
PYTHON
	chmod 700 "$path"
}

run_permission_probe() {
	local shell_command="$1"
	local expected="$2"
	local probe
	local port_file
	local server
	local port
	local config
	local status
	local agora_marker
	local network_marker
	local source_marker
	local wrapper
	local shell_gate

	probe="$(mktemp -d "$TEST_TMP/permission.XXXXXX")"
	shell_command="${shell_command//__PROBE__/$probe}"
	port_file="$probe/port"
	agora_marker="$probe/agora-invoked"
	network_marker="$probe/network-invoked"
	source_marker="$probe/source-invoked"
	mkdir -p -- "$probe/bin" "$probe/shell" "$probe/work" "$probe/home" "$probe/config/opencode" "$probe/data" "$probe/state" "$probe/cache" "$probe/tmp"
	cat >"$probe/bin/agora-real" <<FAKE_AGORA
#!/usr/bin/env bash
: >"$agora_marker"
FAKE_AGORA
	cat >"$probe/bin/curl" <<FAKE_CURL
#!/usr/bin/env bash
: >"$network_marker"
FAKE_CURL
	printf ': >%q\n' "$source_marker" >"$probe/source-command"
	chmod 700 "$probe/bin/agora-real" "$probe/bin/curl" "$probe/source-command"
	wrapper="$probe/bin/agora"
	shell_gate="$probe/shell/bash"
	write_agora_wrapper "$wrapper"
	write_shell_gate "$shell_gate"

	python3 "$TEST_TMP/permission-provider.py" "$port_file" "$shell_command" >"$probe/provider.log" 2>&1 &
	server=$!
	if ! wait_for_file "$port_file"; then
		kill "$server" 2>/dev/null || true
		wait "$server" 2>/dev/null || true
		return 1
	fi
	port="$(<"$port_file")"
	config="$(opencode_permission_config "$shell_gate")"
	config="$(jq -c --arg base_url "http://127.0.0.1:$port/v1" '
		.provider.local = {
			npm: "@ai-sdk/openai-compatible",
			name: "Local permission probe",
			options: {baseURL: $base_url, apiKey: "fake"},
			models: {mock: {name: "Mock", limit: {context: 4096, output: 1024}}}
		}
	' <<<"$config")"

	set +e
	run_isolated_opencode "$probe" "$probe/bin" "$config" '{}' "$probe/bin/agora-real" "$probe/invocations" "$probe/transcript" 'local/mock' "$probe/work" \
		"$HOST_OPENCODE" run \
			--pure \
			--format json \
			--model local/mock \
			--agent "$EVALUATOR_AGENT" \
			--title "Permission probe" \
			--dir "$probe/work" \
			"permission probe" >"$probe/events.jsonl" 2>"$probe/opencode.stderr"
	status=$?
	set -e
	kill "$server" 2>/dev/null || true
	wait "$server" 2>/dev/null || true
	[[ "$status" -eq 0 ]] || return 1

	if [[ "$expected" == "allow" ]]; then
		tool_status_is "$probe/events.jsonl" completed && [[ -e "$agora_marker" ]]
		return
	fi
	if [[ "$expected" == "wrapper-reject" ]]; then
		tool_status_is "$probe/events.jsonl" completed \
			&& tool_exit_is "$probe/events.jsonl" 64 \
			&& [[ -s "$probe/invocations" ]] \
			&& [[ ! -e "$agora_marker" ]]
		return
	fi
	tool_status_is "$probe/events.jsonl" completed \
		&& tool_exit_is "$probe/events.jsonl" 64 \
		&& [[ ! -s "$probe/invocations" ]] \
		&& [[ ! -e "$agora_marker" ]] \
		&& [[ ! -e "$network_marker" ]] \
		&& [[ ! -e "$source_marker" ]] \
		&& [[ ! -e "$probe/redirection" ]] \
		&& [[ ! -e "$probe/arbitrary-write" ]] \
		&& [[ ! -e "$probe/work/arbitrary-write" ]]
}

write_foreign_go_module() {
	local path="$1"
	mkdir -p -- "$path/cmd/agora"
	cat >"$path/go.mod" <<'GO_MOD'
module example.invalid/foreign-agora

go 1.26
GO_MOD
	cat >"$path/cmd/agora/main.go" <<'GO_MAIN'
package main

import "fmt"

func main() {
	fmt.Println("foreign-agora")
}
GO_MAIN
}

boundary_belongs_to_checkout() {
	local boundary="$1"
	local revision="$2"
	local version="$3"

	jq -e --arg root "$TEST_ROOT" --arg revision "$revision" --arg version "$version" '
		.checkout.root == $root
		and .checkout.revision == $revision
		and .executables.agora.version == $version
		and .executables.agora.version != "foreign-agora"
	' "$boundary" >/dev/null
}

run_public_interruption_probe() {
	local signal="$1"
	local expected_status="$2"
	local record_dir="$TEST_TMP/public-${signal,,}"
	local fake="$record_dir/opencode"
	local output="$record_dir/output"
	local status
	local child_pid
	local temp_root
	local script_pid
	local supervisor_pid
	local credential_values

	mkdir -p -- "$record_dir/tmp"
	write_interrupting_opencode "$fake" "$record_dir"
	credential_values="$(credential_values_for_auth "$(auth_content_for_model "$TEST_TMP/auth.json" 'opencode/test-model')")"
	AGORA_EVALUATOR_OPENCODE_BIN="$fake" TMPDIR="$record_dir/tmp" \
		python3 "$TEST_TMP/process-supervisor.py" "$record_dir/script.pid" "$record_dir/status" \
			"$TEST_ROOT/scripts/eval-cli-discovery.sh" \
			--output "$output" \
			--model 'opencode/test-model' \
			--auth-file "$TEST_TMP/auth.json" \
			--timeout '30s' &
	supervisor_pid=$!
	if ! wait_for_file "$record_dir/script.pid"; then
		kill -KILL "$supervisor_pid" 2>/dev/null || true
		wait "$supervisor_pid" 2>/dev/null || true
		return 1
	fi
	script_pid="$(<"$record_dir/script.pid")"
	if ! wait_for_file "$record_dir/ready" 600; then
		kill -KILL "$script_pid" 2>/dev/null || true
		wait "$supervisor_pid" 2>/dev/null || true
		return 1
	fi
	kill -"$signal" "$script_pid"
	wait "$supervisor_pid" || return 1
	[[ -s "$record_dir/status" ]] || return 1
	status="$(<"$record_dir/status")"
	[[ "$status" -eq "$expected_status" ]] || return 1
	[[ ! -e "$output" ]] || return 1
	[[ -s "$record_dir/child.pid" && -s "$record_dir/temp-root" ]] || return 1
	child_pid="$(<"$record_dir/child.pid")"
	temp_root="$(<"$record_dir/temp-root")"
	! kill -0 "$child_pid" 2>/dev/null \
		&& [[ ! -e "$temp_root" ]] \
		&& assert_no_credential_copy "$record_dir" "$credential_values"
}

main() {
	local tmp
	local config
	local probe
	local probe_auth
	local probe_credentials
	local probe_gate
	local probe_config
	local fake
	local auth_file
	local selected_auth
	local credential_values
	local token
	local unrelated
	local output
	local boundary
	local events
	local temp_root
	local real_path
	local wrapper
	local recording_real
	local recording_log
	local transcript
	local planted_output
	local foreign_checkout
	local expected_agora
	local expected_version
	local revision
	local leaking_fake
	local leaking_output
	local failing_fake
	local failing_output
	local failure_status
	local leak_status
	local policy_gate

	tmp="$(mktemp -d "${TMPDIR:-/tmp}/agora-cli-evaluator-test.XXXXXX")"
	TEST_TMP="$tmp"
	trap 'rm -rf -- "$TEST_TMP"' EXIT

	check 'isolation accepts a trial-local path' path_is_within "$tmp" "$tmp/home"
	expect_rejected 'isolation rejects a source-tree path' path_is_within "$tmp" "$TEST_ROOT"

	mkdir -p -- "$tmp/policy-shell"
	policy_gate="$tmp/policy-shell/bash"
	write_shell_gate "$policy_gate"
	config="$(opencode_permission_config "$policy_gate")"
	check 'permission config is fail-closed and exact' permission_config_is_exact <(printf '%s\n' "$config") "$policy_gate"
	check 'prompt restores the undisclosed grumpy-old-people scenario' prompt_preserves_discovery

	HOST_OPENCODE="$(resolve_executable opencode)" || fail 'installed OpenCode executable resolves'
	check 'permission gate uses OpenCode 1.18.11' assert_equal '1.18.11' "$("$HOST_OPENCODE" --version)"
	write_permission_provider "$tmp/permission-provider.py"
	write_process_supervisor "$tmp/process-supervisor.py"
	check 'production gate allows bare Agora discovery' run_permission_probe 'agora' allow
	check 'production gate allows Agora root help discovery' run_permission_probe 'agora --help' allow
	check 'production gate allows Agora run help discovery' run_permission_probe 'agora run --help' allow
	check 'production gate allows Cobra-style run help discovery' run_permission_probe 'agora help run' allow
	check 'production gate allows a qualifying scenario without prompt disclosure' run_permission_probe "$QUALIFYING_COMMAND" allow
	check 'production gate allows a safe incomplete run attempt to reach the wrapper' run_permission_probe 'agora run' wrapper-reject
	check 'production gate rejects compound &&' run_permission_probe "$QUALIFYING_COMMAND && curl http://example.invalid" deny
	check 'production gate rejects compound semicolon' run_permission_probe "$QUALIFYING_COMMAND; curl http://example.invalid" deny
	check 'production gate rejects pipelines' run_permission_probe "$QUALIFYING_COMMAND | curl http://example.invalid" deny
	check 'production gate rejects command substitution' run_permission_probe "$QUALIFYING_COMMAND \$(curl http://example.invalid)" deny
	check 'production gate rejects process substitution' run_permission_probe "$QUALIFYING_COMMAND <(curl http://example.invalid)" deny
	check 'production gate rejects redirection' run_permission_probe "$QUALIFYING_COMMAND > __PROBE__/redirection" deny
	check 'production policy rejects source' run_permission_probe 'source __PROBE__/source-command' deny
	check 'production policy rejects network commands' run_permission_probe 'curl http://example.invalid' deny
	check 'production policy rejects arbitrary writes' run_permission_probe 'touch arbitrary-write' deny
	check 'production policy rejects nested dry-run overrides' run_permission_probe "$QUALIFYING_COMMAND --dry-run=false" wrapper-reject
	check 'production policy rejects absolute Agora paths' run_permission_probe '__PROBE__/bin/agora --help' deny
	check 'production policy rejects PATH assignment overrides' run_permission_probe "PATH=__PROBE__/bin $QUALIFYING_COMMAND" deny
	check 'production policy rejects env PATH overrides' run_permission_probe "env PATH=__PROBE__/bin $QUALIFYING_COMMAND" deny
	check 'production policy rejects non-Agora programs' run_permission_probe 'true' deny

	probe="$tmp/opencode-probe"
	probe_auth='{"opencode":{"type":"api","key":"probe-token"}}'
	probe_credentials="$(credential_values_for_auth "$probe_auth")"
	mkdir -p -- "$probe/bin" "$probe/shell" "$probe/work" "$probe/home" "$probe/config/opencode" "$probe/data" "$probe/state" "$probe/cache" "$probe/tmp"
	probe_gate="$probe/shell/bash"
	write_shell_gate "$probe_gate"
	probe_config="$(opencode_permission_config "$probe_gate")"
	run_isolated_opencode "$probe" "$probe/bin" "$probe_config" "$probe_auth" /bin/true "$probe/invocations" "$probe/transcript" 'opencode/test-model' "$probe/work" "$HOST_OPENCODE" debug config >"$probe/config.json"
	check 'isolation config omits inherited plugins' json_assert '.plugin == []' "$probe/config.json"
	check 'isolation config resolves the exact production gate' permission_config_is_exact "$probe/config.json" "$probe_gate"
	check 'OpenCode config probe does not persist temporary auth' test ! -e "$probe/data/opencode/auth.json"
	check 'OpenCode config probe leaves no temporary auth copy' assert_no_credential_copy "$probe/data" "$probe_credentials"

	fake="$tmp/fake-opencode"
	write_fake_opencode "$fake"
	check 'executable path accepts the configured OpenCode executable' test -x "$(resolve_executable "$fake")"
	expect_rejected 'executable path rejects a missing OpenCode executable' resolve_executable "$tmp/missing-opencode"

	token="boundary-credential-${RANDOM}-${RANDOM}"
	unrelated="unselected-credential-${RANDOM}-${RANDOM}"
	auth_file="$tmp/auth.json"
	jq -n --arg token "$token" --arg unrelated "$unrelated" '{opencode: {type: "api", key: $token}, unrelated: {type: "api", key: $unrelated}}' >"$auth_file"
	selected_auth="$(auth_content_for_model "$auth_file" 'opencode/test-model')"
	credential_values="$(credential_values_for_auth "$selected_auth")"

	foreign_checkout="$tmp/foreign-checkout"
	write_foreign_go_module "$foreign_checkout"
	expected_agora="$tmp/expected-agora"
	go -C "$TEST_ROOT" build -buildvcs=false -o "$expected_agora" ./cmd/agora
	expected_version="$("$expected_agora" --version)"
	revision="$(git -C "$TEST_ROOT" rev-parse HEAD)"
	output="$tmp/trial-output"
	(
		cd "$foreign_checkout"
		run_trial "$output" 'opencode/test-model' '30s' "$fake" "$auth_file"
	) || fail 'provider-free fake outer trial from another Go module'
	boundary="$output/boundary.json"
	events="$output/events.jsonl"

	check 'checkout-root build ignores the caller Go module' boundary_belongs_to_checkout "$boundary" "$revision" "$expected_version"
	check 'isolation passes the fresh home to OpenCode' assert_equal "$(jq -r '.state.home' "$boundary")" "$(jq -r '.home' "$events")"
	check 'isolation passes the fresh data home to OpenCode' assert_equal "$(jq -r '.state.data_home' "$boundary")" "$(jq -r '.data_home' "$events")"
	check 'isolation passes the fresh OpenCode config directory' assert_equal "$(jq -r '.state.opencode_config_dir' "$boundary")" "$(jq -r '.config_dir' "$events")"
	check 'selected model reaches the outer OpenCode invocation' json_assert '.args | contains("--model opencode/test-model")' "$events"
	check 'selected model reaches nested Agora metadata' transcript_uses_model 'opencode/test-model' "$output/agora-transcript.jsonl"
	check 'fresh checkout artifact reaches the wrapper' assert_equal "$(jq -r '.executables.agora.path' "$boundary")" "$(jq -r '.real' "$events")"
	check 'fresh checkout artifact version is recorded from that binary' assert_equal "$(jq -r '.executables.agora.version' "$boundary")" "$(jq -r '.real_version' "$events")"
	check 'outer shell resolves agora to the temporary wrapper' assert_equal "$(jq -r '.nested_agora.wrapper' "$boundary")" "$(jq -r '.wrapper' "$events")"
	check 'outer OpenCode receives the recorded production shell gate' permission_config_is_exact <(jq -r '.config' "$events") "$(jq -r '.nested_agora.command_gate' "$boundary")"
	check 'temporary auth is present only for the isolated child' json_assert '.auth_present == true' "$events"
	check 'temporary auth contains only the selected provider' json_assert '.auth_provider == "opencode" and .auth_provider_count == 1' "$events"
	check 'nested execution produced dry-run content' grep -Fq -- '[DRY RUN]' "$output/agora-transcript.jsonl"

	wrapper="$tmp/direct-wrapper"
	recording_real="$tmp/recording-real"
	recording_log="$tmp/recording-real.log"
	transcript="$tmp/direct-transcript.jsonl"
	write_agora_wrapper "$wrapper"
	write_recording_real "$recording_real"
	REAL_ARGS_LOG="$recording_log" \
		OPENCODE_AUTH_CONTENT="$selected_auth" \
		AGORA_EVALUATOR_REAL="$recording_real" \
		AGORA_EVALUATOR_INVOCATIONS="$tmp/direct-invocations.log" \
		AGORA_EVALUATOR_TRANSCRIPT="$transcript" \
		AGORA_EVALUATOR_MODEL='opencode/test-model' \
		AGORA_EVALUATOR_WORKDIR="$tmp/work" \
		"$wrapper" run --auto quick --research --yes --topic weather
	check 'nested Agora receives no temporary OpenCode auth' grep -Fq -- 'auth=absent' "$recording_log"
	check 'nested model rule injects the selected model' grep -Fq -- '--model opencode/test-model' "$recording_log"
	check 'nested dry-run rule injects --dry-run' grep -Fq -- '--dry-run' "$recording_log"
	check 'nested output rule injects the fresh transcript path' grep -Fq -- "--output $transcript" "$recording_log"
	: >"$recording_log"
	expect_rejected 'nested dry-run rule rejects a caller dry-run override' env \
		REAL_ARGS_LOG="$recording_log" \
		AGORA_EVALUATOR_REAL="$recording_real" \
		AGORA_EVALUATOR_INVOCATIONS="$tmp/direct-invocations.log" \
		AGORA_EVALUATOR_TRANSCRIPT="$transcript" \
		AGORA_EVALUATOR_MODEL='opencode/test-model' \
		AGORA_EVALUATOR_WORKDIR="$tmp/work" \
		"$wrapper" run --auto quick --research --yes --topic weather --dry-run=false
	check 'rejected nested dry-run override does not launch Agora' test ! -s "$recording_log"

	leaking_fake="$tmp/leaking-opencode"
	leaking_output="$tmp/leaking-output"
	write_leaf_leaking_opencode "$leaking_fake"
	set +e
	(run_trial "$leaking_output" 'opencode/test-model' '30s' "$leaking_fake" "$auth_file")
	leak_status=$?
	set -e
	check 'credential leaf leak changes a successful outer status to failure' assert_equal 2 "$leak_status"
	check 'credential leaf leak removes the complete trial output' test ! -e "$leaking_output"

	failing_fake="$tmp/failing-opencode"
	failing_output="$tmp/failing-output"
	write_failing_opencode "$failing_fake" 23
	set +e
	(run_trial "$failing_output" 'opencode/test-model' '30s' "$failing_fake" "$auth_file")
	failure_status=$?
	set -e
	check 'clean failure preserves the outer exit status' assert_equal 23 "$failure_status"
	check 'clean failure retains credential-free output' test -d "$failing_output"
	check 'clean failure output contains no selected credential leaf' assert_no_credential_copy "$failing_output" "$credential_values"

	check 'public-PID TERM preserves 143, reaps the child group, and removes secret state' run_public_interruption_probe TERM 143
	check 'public-PID INT preserves 130, reaps the child group, and removes secret state' run_public_interruption_probe INT 130

	temp_root="$(jq -r '.state.temporary_root' "$boundary")"
	real_path="$(jq -r '.executables.agora.path' "$boundary")"
	check 'normal cleanup removes the temporary evaluator root' test ! -e "$temp_root"
	check 'normal cleanup removes the temporary checkout binary' test ! -e "$real_path"
	check 'normal cleanup leaves no selected credential leaf in output' assert_no_credential_copy "$output" "$credential_values"
	check 'normal cleanup retains credential-free output' remove_output_on_credential_copy "$output" "$credential_values"
	planted_output="$tmp/planted-credential-output"
	mkdir -- "$planted_output"
	printf '%s\n' "$token" >"$planted_output/credential-copy"
	expect_rejected 'cleanup rejects a planted credential leaf' remove_output_on_credential_copy "$planted_output" "$credential_values"
	check 'cleanup removes a planted credential leaf' test ! -e "$planted_output"

	printf 'provider-free boundary self-test passed (%d checks)\n' "$checks"
}

main "$@"
