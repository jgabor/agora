#!/usr/bin/env bash
# Maintainer boundary for one isolated Agora CLI-discovery trial.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_MODEL="${AGORA_EVALUATOR_MODEL:-opencode/big-pickle}"
EVALUATOR_AGENT="agora-evaluator"
PROMPT="Run \`agora\` and initiate a quick debate between grumpy old people on the latest weather report."

usage() {
	cat <<EOF
Usage:
  scripts/eval-cli-discovery.sh --output DIR [options]
  scripts/eval-cli-discovery.sh --self-test

Run one isolated OpenCode evaluation trial against a freshly built Agora
checkout. The outer OpenCode call can use the selected provider. Every nested
Agora run is normalized to a dry run with a fresh transcript path.

Options:
  --output DIR       New directory for trial evidence (required for a trial).
  --model PROVIDER/MODEL
                     Model for the outer OpenCode call and nested Agora run
                     (default: $DEFAULT_MODEL).
  --auth-file PATH   OpenCode auth.json source. Defaults to the current user's
                     XDG data auth file; only the selected provider is passed
                     to the trial in memory.
  --timeout DURATION Per-trial timeout accepted by GNU timeout (default: 5m).
  --self-test        Run provider-free boundary checks.
  -h, --help         Show this help.

Set AGORA_EVALUATOR_OPENCODE_BIN to select the OpenCode executable. The trial
records executable paths and versions in DIR/boundary.json. It never copies
credentials to disk.
EOF
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 2
}

require_value() {
	local option="$1"
	local value="${2:-}"
	[[ -n "$value" ]] || die "$option requires a value"
}

resolve_executable() {
	local candidate="$1"
	local path=""

	if [[ "$candidate" == */* ]]; then
		path="$candidate"
	else
		path="$(command -v -- "$candidate" 2>/dev/null || true)"
	fi
	[[ -n "$path" && -x "$path" ]] || return 1
	readlink -f -- "$path"
}

provider_for_model() {
	local model="$1"
	local provider="${model%%/*}"
	[[ "$model" == */* && -n "$provider" && "${model#*/}" != "$model" && -n "${model#*/}" ]] || return 1
	printf '%s\n' "$provider"
}

auth_content_for_model() {
	local auth_file="$1"
	local model="$2"
	local provider

	provider="$(provider_for_model "$model")" || return 1
	[[ -r "$auth_file" ]] || return 1
	jq -ce --arg provider "$provider" '
		if type == "object" and has($provider) then
			{($provider): .[$provider]}
		else
			error("no auth entry for provider " + $provider)
		end
	' "$auth_file"
}

path_is_within() {
	local root="${1%/}"
	local path="$2"
	[[ "$path" == "$root" || "$path" == "$root/"* ]]
}

opencode_permission_config() {
	local shell_gate="$1"

	jq -nc --arg agent "$EVALUATOR_AGENT" --arg shell "$shell_gate" '
		{
			"$schema": "https://opencode.ai/config.json",
			shell: $shell,
			permission: {
				"*": "deny",
				bash: "allow"
			},
			agent: {
				($agent): {
					mode: "primary"
				}
			}
		}
	'
}

write_shell_gate() {
	local path="$1"

	cat >"$path" <<'GATE'
#!/bin/bash
set -euo pipefail

: "${AGORA_EVALUATOR_WRAPPER:?}"

reject() {
	printf 'agora evaluator gate: %s\n' "$*" >&2
	exit 64
}

[[ "$#" -eq 2 && "$1" == "-c" ]] || reject "unsupported shell invocation"
command_text="$2"
argv=()
word=""
state=plain
started=0

# OpenCode passes the model's command as argv[2]. Parse only shell words here;
# never pass the text to eval, -c, or another shell parser.
for ((i=0; i<${#command_text}; i++)); do
	char="${command_text:i:1}"
	case "$char" in
	$'\n' | $'\r' | ';' | '&' | '|' | '<' | '>' | '$' | '`' | '(' | ')' | '\\')
		reject "shell operators, substitution, redirection, and escapes are not permitted"
		;;
	esac

	case "$state:$char" in
	plain:"'" )
		state=single
		started=1
		;;
	plain:'"')
		state=double
		started=1
		;;
	plain:$' ' | plain:$'\t')
		if ((started)); then
			argv+=("$word")
			word=""
			started=0
		fi
		;;
	single:"'")
		state=plain
		;;
	double:'"')
		state=plain
		;;
	*)
		[[ "$char" != [[:cntrl:]] ]] || reject "control characters are not permitted"
		word+="$char"
		started=1
		;;
	esac
done

[[ "$state" == plain ]] || reject "unterminated quote"
if ((started)); then
	argv+=("$word")
fi
[[ "${#argv[@]}" -gt 0 ]] || reject "an Agora command is required"
[[ "${argv[0]}" == "agora" ]] || reject "only the checkout Agora wrapper is permitted"

exec "$AGORA_EVALUATOR_WRAPPER" "${argv[@]:1}"
GATE
	chmod 700 "$path"
}

write_agora_wrapper() {
	local path="$1"

	cat >"$path" <<'WRAPPER'
#!/usr/bin/env bash
set -euo pipefail

: "${AGORA_EVALUATOR_REAL:?}"
: "${AGORA_EVALUATOR_INVOCATIONS:?}"
: "${AGORA_EVALUATOR_TRANSCRIPT:?}"
: "${AGORA_EVALUATOR_MODEL:?}"
: "${AGORA_EVALUATOR_WORKDIR:?}"

reject() {
	printf 'agora evaluator: %s\n' "$*" >&2
	exit 64
}

{
	printf '%s' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	printf ' %q' "$@"
	printf '\n'
} >>"$AGORA_EVALUATOR_INVOCATIONS"

# The outer OpenCode process needs this temporary credential. Nested dry-run
# Agora processes never do, so do not pass it beyond this wrapper.
unset OPENCODE_AUTH_CONTENT OPENCODE_API_KEY

if [[ "$#" -eq 0 || ("$#" -eq 1 && ("$1" == "--help" || "$1" == "-h" || "$1" == "help")) ]]; then
	exec "$AGORA_EVALUATOR_REAL" --help
fi

if [[ "$#" -eq 2 && "$1" == "help" && "$2" == "run" ]]; then
	exec "$AGORA_EVALUATOR_REAL" run --help
fi

[[ "${1:-}" == "run" ]] || reject "only 'agora --help' and 'agora run' are permitted"
shift

if [[ "$#" -eq 1 && ("$1" == "--help" || "$1" == "-h") ]]; then
	exec "$AGORA_EVALUATOR_REAL" run --help
fi

auto=""
topic=""
yes=0
research=0

while (($# > 0)); do
	case "$1" in
	--auto)
		(($# >= 2)) || reject "--auto requires a value"
		[[ -z "$auto" ]] || reject "--auto may appear once"
		auto="$2"
		shift 2
		;;
	--auto=*)
		[[ -z "$auto" ]] || reject "--auto may appear once"
		auto="${1#*=}"
		shift
		;;
	-t | --topic)
		(($# >= 2)) || reject "$1 requires a value"
		[[ -z "$topic" ]] || reject "--topic may appear once"
		topic="$2"
		shift 2
		;;
	--topic=* | -t=*)
		[[ -z "$topic" ]] || reject "--topic may appear once"
		topic="${1#*=}"
		shift
		;;
	--yes | --yes=true)
		yes=1
		shift
		;;
	--research | --research=true)
		research=1
		shift
		;;
	*)
		reject "unsupported argument: $1"
		;;
	esac
done

[[ "$auto" == "quick" ]] || reject "the evaluator requires --auto quick"
((yes)) || reject "the evaluator requires --yes"
((research)) || reject "the evaluator requires --research"
[[ -n "$topic" ]] || reject "the evaluator requires a non-empty --topic"

exec "$AGORA_EVALUATOR_REAL" run \
	--auto quick \
	--topic "$topic" \
	--yes \
	--research \
	--model "$AGORA_EVALUATOR_MODEL" \
	--output "$AGORA_EVALUATOR_TRANSCRIPT" \
	--dry-run \
	--no-context \
	--workdir "$AGORA_EVALUATOR_WORKDIR"
WRAPPER
	chmod 700 "$path"
}

assert_no_credential_copy() {
	local path="$1"
	local credentials="$2"
	local status

	[[ -e "$path" ]] || return 0
	if LC_ALL=C grep -rFq -f <(jq -r '.[]' <<<"$credentials") -- "$path" >/dev/null 2>&1; then
		return 1
	fi
	status=$?
	[[ "$status" -eq 1 ]] && return 0
	return "$status"
}

remove_output_on_credential_copy() {
	local output="$1"
	local credentials="$2"
	local status

	assert_no_credential_copy "$output" "$credentials" && return 0
	status=$?
	rm -rf -- "$output" || return 2
	return "$status"
}

credential_values_for_auth() {
	local auth_content="$1"

	jq -ce '
		to_entries as $providers
		| if ($providers | length) != 1 then
			error("expected one selected provider credential")
		  else
			$providers[0].value as $auth
			| if $auth.type == "api" then
				[$auth.key]
			  elif $auth.type == "oauth" then
				[$auth.refresh, $auth.access]
			  elif $auth.type == "wellknown" then
				[$auth.key, $auth.token]
			  else
				error("unsupported selected provider credential")
			  end
			| if all(.[]; type == "string" and length > 0) then unique
			  else error("selected provider credential has an empty secret field")
			  end
		  end
	' <<<"$auth_content"
}

run_isolated_opencode() (
	local tmp_root="$1"
	local wrapper_dir="$2"
	local config_json="$3"
	local auth_content="$4"
	local agora_real="$5"
	local invocations="$6"
	local transcript="$7"
	local model="$8"
	local workdir="$9"
	shift 9

	exec env -i \
		PATH="$wrapper_dir:$PATH" \
		HOME="$tmp_root/home" \
		TMPDIR="$tmp_root/tmp" \
		XDG_CONFIG_HOME="$tmp_root/config" \
		XDG_DATA_HOME="$tmp_root/data" \
		XDG_STATE_HOME="$tmp_root/state" \
		XDG_CACHE_HOME="$tmp_root/cache" \
		OPENCODE_TEST_HOME="$tmp_root/home" \
		OPENCODE_CONFIG_DIR="$tmp_root/config/opencode" \
		OPENCODE_CONFIG_CONTENT="$config_json" \
		OPENCODE_DISABLE_PROJECT_CONFIG=1 \
		OPENCODE_PURE=1 \
		OPENCODE_DISABLE_AUTOUPDATE=1 \
		OPENCODE_DISABLE_AUTOCOMPACT=1 \
		OPENCODE_DISABLE_MODELS_FETCH=1 \
		OPENCODE_AUTH_CONTENT="$auth_content" \
		NO_COLOR=1 \
		AGORA_EVALUATOR_REAL="$agora_real" \
		AGORA_EVALUATOR_WRAPPER="$wrapper_dir/agora" \
		AGORA_EVALUATOR_INVOCATIONS="$invocations" \
		AGORA_EVALUATOR_TRANSCRIPT="$transcript" \
		AGORA_EVALUATOR_MODEL="$model" \
		AGORA_EVALUATOR_WORKDIR="$workdir" \
		"$@"
)

write_boundary() {
	local path="$1"
	local tmp_root="$2"
	local output="$3"
	local model="$4"
	local opencode_bin="$5"
	local opencode_version="$6"
	local agora_bin="$7"
	local agora_version="$8"
	local wrapper="$9"
	local workdir="${10}"
	local shell_gate="${11}"
	local provider
	local config_json
	local revision

	provider="$(provider_for_model "$model")" || die "--model must use provider/model form"
	config_json="$(opencode_permission_config "$shell_gate")"
	revision="$(git -C "$ROOT" rev-parse HEAD)"

	jq -n \
		--arg root "$ROOT" \
		--arg revision "$revision" \
		--arg tmp_root "$tmp_root" \
		--arg model "$model" \
		--arg provider "$provider" \
		--arg opencode_bin "$opencode_bin" \
		--arg opencode_version "$opencode_version" \
		--arg agora_bin "$agora_bin" \
		--arg agora_version "$agora_version" \
		--arg wrapper "$wrapper" \
		--arg shell_gate "$shell_gate" \
		--arg workdir "$workdir" \
		--arg output "$output" \
		--argjson config "$config_json" \
		'{
			checkout: {root: $root, revision: $revision},
			executables: {
				agora: {path: $agora_bin, version: $agora_version},
				opencode: {path: $opencode_bin, version: $opencode_version}
			},
			model: $model,
			authentication: {
				provider: $provider,
				transport: "OPENCODE_AUTH_CONTENT",
				persisted: false
			},
			state: {
				temporary_root: $tmp_root,
				home: ($tmp_root + "/home"),
				config_home: ($tmp_root + "/config"),
				data_home: ($tmp_root + "/data"),
				state_home: ($tmp_root + "/state"),
				cache_home: ($tmp_root + "/cache"),
				opencode_config_dir: ($tmp_root + "/config/opencode"),
				workdir: $workdir
			},
			output: {
				directory: $output,
				events: ($output + "/events.jsonl"),
				stderr: ($output + "/opencode.stderr"),
				invocations: ($output + "/agora-invocations.log"),
				transcript: ($output + "/agora-transcript.jsonl")
			},
			permissions: $config.permission,
			nested_agora: {
				wrapper: $wrapper,
				command_gate: $shell_gate,
				dry_run: true,
				context_enabled: false,
				fixed_output: true,
				fixed_model: true
			}
		}' >"$path"
}

run_trial() {
	local output="$1"
	local model="$2"
	local timeout_duration="$3"
	local opencode_request="$4"
	local auth_file="$5"
	local parent
	local opencode_bin
	local opencode_version
	local auth_content=""
	local credential_values=""
	local tmp_root=""
	local agora_bin
	local agora_version
	local wrapper
	local shell_gate
	local workdir
	local config_json
	local outer_status
	local outer_pid=""
	local output_created=0
	local remove_output=0

	# shellcheck disable=SC2329 # Called by cleanup and the signal handler below.
	terminate_outer_process_group() {
		local pid="$outer_pid"
		local signal="${1:-TERM}"
		local attempt

		[[ -n "$pid" ]] || return 0
		kill -"$signal" -- "-$pid" 2>/dev/null || true
		for ((attempt=0; attempt<20; attempt++)); do
			kill -0 -- "-$pid" 2>/dev/null || break
			sleep 0.01
		done
		if kill -0 -- "-$pid" 2>/dev/null; then
			kill -KILL -- "-$pid" 2>/dev/null || true
		fi
		wait "$pid" 2>/dev/null || true
		outer_pid=""
	}

	# shellcheck disable=SC2329 # Called through the EXIT trap below.
	cleanup() {
		local status=$?

		trap - EXIT HUP INT TERM
		terminate_outer_process_group
		if ((remove_output && output_created)); then
			if ! rm -rf -- "$output"; then
				if ((status == 0)); then
					status=2
				fi
			fi
		elif ((output_created)) && [[ -n "$credential_values" ]]; then
			if ! remove_output_on_credential_copy "$output" "$credential_values"; then
				printf 'error: credential content appeared in trial output; removed it\n' >&2
				if ((status == 0)); then
					status=2
				fi
			fi
		fi
		auth_content=''
		credential_values=''
		unset auth_content credential_values
		if [[ -n "$tmp_root" ]]; then
			rm -rf -- "$tmp_root" || true
		fi
		exit "$status"
	}

	# shellcheck disable=SC2329 # Called through the signal traps below.
	interrupt_trial() {
		local status="$1"
		local signal="$2"

		remove_output=1
		trap - HUP INT TERM
		terminate_outer_process_group "$signal"
		exit "$status"
	}

	trap cleanup EXIT
	trap 'interrupt_trial 129 HUP' HUP
	trap 'interrupt_trial 130 INT' INT
	trap 'interrupt_trial 143 TERM' TERM

	for command in go jq timeout grep setsid; do
		command -v "$command" >/dev/null 2>&1 || die "$command is required"
	done

	opencode_bin="$(resolve_executable "$opencode_request")" || die "opencode executable is required: $opencode_request"
	opencode_version="$("$opencode_bin" --version)"
	auth_content="$(auth_content_for_model "$auth_file" "$model")" || die "auth file has no readable entry for $(provider_for_model "$model" 2>/dev/null || printf 'the selected') provider"
	credential_values="$(credential_values_for_auth "$auth_content")" || die "selected provider auth has no supported credential leaves"

	if [[ "$output" != /* ]]; then
		output="$ROOT/$output"
	fi
	parent="$(dirname -- "$output")"
	[[ -d "$parent" ]] || die "output parent does not exist: $parent"
	[[ ! -e "$output" ]] || die "output must be a new path: $output"
	mkdir -- "$output"
	output_created=1

	tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/agora-cli-evaluator.XXXXXX")"
	mkdir -p -- "$tmp_root/bin" "$tmp_root/shell" "$tmp_root/work" "$tmp_root/home" "$tmp_root/config/opencode" "$tmp_root/data" "$tmp_root/state" "$tmp_root/cache" "$tmp_root/tmp"
	for path in "$tmp_root/bin" "$tmp_root/shell" "$tmp_root/work" "$tmp_root/home" "$tmp_root/config" "$tmp_root/data" "$tmp_root/state" "$tmp_root/cache"; do
		path_is_within "$tmp_root" "$path" || die "trial state escaped its temporary root"
	done

	agora_bin="$tmp_root/bin/agora-real"
	go -C "$ROOT" build -buildvcs=false -o "$agora_bin" ./cmd/agora
	agora_version="$("$agora_bin" --version)"
	wrapper="$tmp_root/bin/agora"
	write_agora_wrapper "$wrapper"
	shell_gate="$tmp_root/shell/bash"
	write_shell_gate "$shell_gate"
	workdir="$tmp_root/work"
	config_json="$(opencode_permission_config "$shell_gate")"

	write_boundary "$output/boundary.json" "$tmp_root" "$output" "$model" "$opencode_bin" "$opencode_version" "$agora_bin" "$agora_version" "$wrapper" "$workdir" "$shell_gate"
	: >"$output/agora-invocations.log"

	set +e
	# Start setsid here so outer_pid is the child process-group leader.
	setsid --wait \
		env -i \
			PATH="$tmp_root/bin:$PATH" \
			HOME="$tmp_root/home" \
			TMPDIR="$tmp_root/tmp" \
			XDG_CONFIG_HOME="$tmp_root/config" \
			XDG_DATA_HOME="$tmp_root/data" \
			XDG_STATE_HOME="$tmp_root/state" \
			XDG_CACHE_HOME="$tmp_root/cache" \
			OPENCODE_TEST_HOME="$tmp_root/home" \
			OPENCODE_CONFIG_DIR="$tmp_root/config/opencode" \
			OPENCODE_CONFIG_CONTENT="$config_json" \
			OPENCODE_DISABLE_PROJECT_CONFIG=1 \
			OPENCODE_PURE=1 \
			OPENCODE_DISABLE_AUTOUPDATE=1 \
			OPENCODE_DISABLE_AUTOCOMPACT=1 \
			OPENCODE_DISABLE_MODELS_FETCH=1 \
			OPENCODE_AUTH_CONTENT="$auth_content" \
			NO_COLOR=1 \
			AGORA_EVALUATOR_REAL="$agora_bin" \
			AGORA_EVALUATOR_WRAPPER="$wrapper" \
			AGORA_EVALUATOR_INVOCATIONS="$output/agora-invocations.log" \
			AGORA_EVALUATOR_TRANSCRIPT="$output/agora-transcript.jsonl" \
			AGORA_EVALUATOR_MODEL="$model" \
			AGORA_EVALUATOR_WORKDIR="$workdir" \
		timeout "$timeout_duration" "$opencode_bin" run \
			--pure \
			--format json \
			--model "$model" \
			--agent "$EVALUATOR_AGENT" \
			--title "Agora CLI discovery evaluation" \
			--dir "$workdir" \
			"$PROMPT" \
			>"$output/events.jsonl" 2>"$output/opencode.stderr" &
	outer_pid=$!
	wait "$outer_pid"
	outer_status=$?
	set -e

	exit "$outer_status"
}

main() {
	local model="$DEFAULT_MODEL"
	local output=""
	local timeout_duration="5m"
	local host_data_home="${XDG_DATA_HOME:-${HOME:?}/.local/share}"
	local auth_file="$host_data_home/opencode/auth.json"
	local opencode_request="${AGORA_EVALUATOR_OPENCODE_BIN:-opencode}"

	while (($# > 0)); do
		case "$1" in
		--output)
			require_value "$1" "${2:-}"
			output="$2"
			shift 2
			;;
		--model)
			require_value "$1" "${2:-}"
			model="$2"
			shift 2
			;;
		--auth-file)
			require_value "$1" "${2:-}"
			auth_file="$2"
			shift 2
			;;
		--timeout)
			require_value "$1" "${2:-}"
			timeout_duration="$2"
			shift 2
			;;
		--self-test)
			(($# == 1)) || die "--self-test cannot be combined with trial options"
			exec "$ROOT/scripts/eval-cli-discovery-test.sh"
			;;
		-h | --help)
			usage
			return 0
			;;
		*)
			die "unknown option: $1"
			;;
		esac
	done

	[[ -n "$output" ]] || die "--output is required"
	run_trial "$output" "$model" "$timeout_duration" "$opencode_request" "$auth_file"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
