#!/usr/bin/env bash
# Maintainer boundary for one isolated Agora CLI-discovery trial.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_MODEL="${AGORA_EVALUATOR_MODEL:-opencode/big-pickle}"
EVALUATOR_AGENT="agora-evaluator"
PROMPT="Run \`agora\` and initiate a quick debate between grumpy old people on the latest weather report."
EVALUATOR_RESULT_ROOT_RELATIVE=".agora-evaluator/cli-discovery"
RESULT_SCHEMA="agora.cli-discovery.result"
RESULT_SCHEMA_VERSION=1
RESULT_FILENAME="result.json"

usage() {
	cat <<EOF
Usage:
  scripts/eval-cli-discovery.sh --output DIR [options]
  scripts/eval-cli-discovery.sh --analyze DIR [--quiet]
  scripts/eval-cli-discovery.sh --self-test
  scripts/eval-cli-discovery.sh --analysis-self-test

Run one isolated OpenCode evaluation trial against a freshly built Agora
checkout. The outer OpenCode call can use the selected provider. Every nested
Agora run is normalized to a dry run with a fresh transcript path.

Modes:
  A trial requires --output DIR. DIR must be new and its parent must exist.
  --analyze DIR reruns deterministic analysis without OpenCode or a provider;
  it cannot be combined with trial options. Each self-test is standalone and
  provider-free.

Metric:
  Every first valid, unique OpenCode part.id tool call gets an ordinal. Failed,
  denied, and spoofing attempts before the first qualifying run also get one.
  Exact duplicate lifecycle snapshots do not add an ordinal; updates retain it.
  The first completed qualifying wrapper run freezes the score; later calls do
  not change it.

Results:
  Analysis writes DIR/$RESULT_FILENAME, an $RESULT_SCHEMA v$RESULT_SCHEMA_VERSION
  result with provenance, a deterministic checklist and trace, known-or-unknown
  evidence fields, and relative raw-evidence references. For uncommitted trial
  evidence, use a new child of $EVALUATOR_RESULT_ROOT_RELATIVE/ (ignored).

Live trial prerequisites:
  --output requires go, jq, GNU timeout, grep, setsid, a resolvable OpenCode
  executable, and supported authentication for its selected provider. The tested
  OpenCode boundary is 1.18.11; validate other versions separately.
  --analyze and --analysis-self-test do not launch OpenCode or need provider
  authentication. --self-test is provider-free but needs the runtime tools and
  a local OpenCode 1.18.11 loopback; it does not need provider authentication.

Options:
  --output DIR       New directory for trial evidence. It must not exist and
                     its parent must exist (required for a trial).
  --analyze DIR      Analyze captured trial evidence without launching OpenCode
                     or a provider. Writes DIR/$RESULT_FILENAME.
  --model PROVIDER/MODEL
                     Model for the outer OpenCode call and nested Agora run
                     (default: $DEFAULT_MODEL).
  --auth-file PATH   OpenCode auth.json fallback. Defaults to the current
                     user's XDG data auth file. For provider opencode, a
                     nonempty inherited OPENCODE_API_KEY takes precedence.
                     The selected credential is staged only in isolated
                     temporary auth state and removed after the outer process.
  --timeout DURATION Per-trial timeout accepted by GNU timeout (default: 5m).
  --self-test        Run provider-free boundary and analysis checks.
  --analysis-self-test
                     Run only offline deterministic analysis checks.
  --quiet            On successful trial or analysis, print only the result
                     path. Not valid with a self-test.
  -h, --help         Show this help.

AGORA_EVALUATOR_MODEL changes the default model. Set
AGORA_EVALUATOR_OPENCODE_BIN to select the OpenCode executable. The trial
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

SELECTED_AUTH_SOURCE=""
SELECTED_AUTH_SECRET=""
SELECTED_CREDENTIAL_VALUES=""

credential_values_for_environment_key() {
	local key="$1"

	printf '%s' "$key" | jq -Rsc '[.]'
}

select_auth_for_model() {
	local auth_file="$1"
	local model="$2"
	local provider
	local auth_content

	SELECTED_AUTH_SOURCE=""
	SELECTED_AUTH_SECRET=""
	SELECTED_CREDENTIAL_VALUES=""
	provider="$(provider_for_model "$model")" || return 1
	if [[ "$provider" == "opencode" && -n "${OPENCODE_API_KEY:-}" ]]; then
		SELECTED_AUTH_SOURCE="environment"
		SELECTED_AUTH_SECRET="$OPENCODE_API_KEY"
		SELECTED_CREDENTIAL_VALUES="$(credential_values_for_environment_key "$OPENCODE_API_KEY")" || return 1
		return 0
	fi

	auth_content="$(auth_content_for_model "$auth_file" "$model")" || return 1
	SELECTED_CREDENTIAL_VALUES="$(credential_values_for_auth "$auth_content")" || return 1
	SELECTED_AUTH_SOURCE="auth_file"
	SELECTED_AUTH_SECRET="$auth_content"
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

# Keep the parser used for evidence qualification identical to the trial shell
# gate. It accepts shell words only, never shell syntax.
PARSED_SHELL_WORDS=()
SHELL_PARSE_ERROR=""

parse_shell_words() {
	local command_text="$1"
	local word=""
	local state=plain
	local started=0
	local char
	local i

	PARSED_SHELL_WORDS=()
	SHELL_PARSE_ERROR=""
	for ((i=0; i<${#command_text}; i++)); do
		char="${command_text:i:1}"
		case "$char" in
		$'\n' | $'\r' | $';' | $'&' | $'|' | $'<' | $'>' | $'$' | $'\x60' | $'(' | $')' | $'\\')
			SHELL_PARSE_ERROR="shell operators, substitution, redirection, and escapes are not permitted"
			return 1
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
				PARSED_SHELL_WORDS+=("$word")
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
			if [[ "$char" == [[:cntrl:]] ]]; then
				SHELL_PARSE_ERROR="control characters are not permitted"
				return 1
			fi
			word+="$char"
			started=1
			;;
		esac
	done

	if [[ "$state" != plain ]]; then
		SHELL_PARSE_ERROR="unterminated quote"
		return 1
	fi
	if ((started)); then
		PARSED_SHELL_WORDS+=("$word")
	fi
	if ((${#PARSED_SHELL_WORDS[@]} == 0)); then
		SHELL_PARSE_ERROR="an Agora command is required"
		return 1
	fi
}

QUALIFYING_TOPIC=""
QUALIFYING_COMMAND_ERROR=""

# This is the exact accepted non-help run shape of the production wrapper.
# It is used both by the wrapper and by offline analysis, so qualification
# cannot widen beyond the enforced nested-run policy.
validate_qualifying_agora_args() {
	local auto=""
	local topic=""
	local yes=0
	local research=0

	QUALIFYING_TOPIC=""
	QUALIFYING_COMMAND_ERROR=""
	if [[ "${1:-}" != "run" ]]; then
		QUALIFYING_COMMAND_ERROR="only 'agora --help' and 'agora run' are permitted"
		return 1
	fi
	shift

	while (($# > 0)); do
		case "$1" in
		--auto)
			if (($# < 2)); then
				QUALIFYING_COMMAND_ERROR="--auto requires a value"
				return 1
			fi
			if [[ -n "$auto" ]]; then
				QUALIFYING_COMMAND_ERROR="--auto may appear once"
				return 1
			fi
			auto="$2"
			shift 2
			;;
		--auto=*)
			if [[ -n "$auto" ]]; then
				QUALIFYING_COMMAND_ERROR="--auto may appear once"
				return 1
			fi
			auto="${1#*=}"
			shift
			;;
		-t | --topic)
			if (($# < 2)); then
				QUALIFYING_COMMAND_ERROR="$1 requires a value"
				return 1
			fi
			if [[ -n "$topic" ]]; then
				QUALIFYING_COMMAND_ERROR="--topic may appear once"
				return 1
			fi
			topic="$2"
			shift 2
			;;
		--topic=* | -t=*)
			if [[ -n "$topic" ]]; then
				QUALIFYING_COMMAND_ERROR="--topic may appear once"
				return 1
			fi
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
			QUALIFYING_COMMAND_ERROR="unsupported argument: $1"
			return 1
			;;
		esac
	done

	if [[ "$auto" != quick ]]; then
		QUALIFYING_COMMAND_ERROR="the evaluator requires --auto quick"
		return 1
	fi
	if ((yes == 0)); then
		QUALIFYING_COMMAND_ERROR="the evaluator requires --yes"
		return 1
	fi
	if ((research == 0)); then
		QUALIFYING_COMMAND_ERROR="the evaluator requires --research"
		return 1
	fi
	if [[ -z "$topic" ]]; then
		QUALIFYING_COMMAND_ERROR="the evaluator requires a non-empty --topic"
		return 1
	fi
	QUALIFYING_TOPIC="$topic"
}

is_qualifying_agora_command() {
	local command_text="$1"

	parse_shell_words "$command_text" || {
		: "$SHELL_PARSE_ERROR"
		return 1
	}
	[[ "${PARSED_SHELL_WORDS[0]}" == agora ]] || return 1
	validate_qualifying_agora_args "${PARSED_SHELL_WORDS[@]:1}" || {
		: "$QUALIFYING_COMMAND_ERROR"
		return 1
	}
	: "$QUALIFYING_TOPIC"
}

write_shell_gate() {
	local path="$1"

	{
		cat <<'GATE'
#!/bin/bash
set -euo pipefail

# Record presence only, before any credential cleanup or command handling.
: "${AGORA_EVALUATOR_SHELL_ENV_LOG:?}"
{
	if [[ -v OPENCODE_API_KEY ]]; then
		printf 'OPENCODE_API_KEY=present\n'
	else
		printf 'OPENCODE_API_KEY=absent\n'
	fi
	if [[ -v OPENCODE_AUTH_CONTENT ]]; then
		printf 'OPENCODE_AUTH_CONTENT=present\n'
	else
		printf 'OPENCODE_AUTH_CONTENT=absent\n'
	fi
} >>"$AGORA_EVALUATOR_SHELL_ENV_LOG"

# Defense in depth. The isolated OpenCode process should start without either
# variable, so its shell child must already report both as absent above.
unset OPENCODE_API_KEY OPENCODE_AUTH_CONTENT
: "${AGORA_EVALUATOR_WRAPPER:?}"

reject() {
	printf 'agora evaluator gate: %s\n' "$*" >&2
	exit 64
}

[[ "$#" -eq 2 && "$1" == "-c" ]] || reject "unsupported shell invocation"
command_text="$2"

# OpenCode passes the model's command as argv[2]. Parse only shell words here;
# never pass the text to eval, -c, or another shell parser.
GATE
		declare -f parse_shell_words
		cat <<'GATE'
parse_shell_words "$command_text" || reject "$SHELL_PARSE_ERROR"
[[ "${PARSED_SHELL_WORDS[0]}" == "agora" ]] || reject "only the checkout Agora wrapper is permitted"

exec "$AGORA_EVALUATOR_WRAPPER" "${PARSED_SHELL_WORDS[@]:1}"
GATE
	} >"$path"
	chmod 700 "$path"
}

write_agora_wrapper() {
	local path="$1"

	{
		cat <<'WRAPPER'
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
WRAPPER
		declare -f validate_qualifying_agora_args
		cat <<'WRAPPER'

if [[ "$#" -eq 0 || ("$#" -eq 1 && ("$1" == "--help" || "$1" == "-h" || "$1" == "help")) ]]; then
	exec "$AGORA_EVALUATOR_REAL" --help
fi

if [[ "$#" -eq 2 && "$1" == "help" && "$2" == "run" ]]; then
	exec "$AGORA_EVALUATOR_REAL" run --help
fi

[[ "${1:-}" == "run" ]] || reject "only 'agora --help' and 'agora run' are permitted"
if [[ "$#" -eq 2 && ("$2" == "--help" || "$2" == "-h") ]]; then
	exec "$AGORA_EVALUATOR_REAL" run --help
fi

validate_qualifying_agora_args "$@" || reject "$QUALIFYING_COMMAND_ERROR"
topic="$QUALIFYING_TOPIC"

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
	} >"$path"
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

write_auth_bootstrap() {
	local path="$1"

	cat >"$path" <<'BOOTSTRAP'
#!/usr/bin/env bash
set -euo pipefail

auth_source="${1:?}"
shift
unset OPENCODE_API_KEY OPENCODE_AUTH_CONTENT
auth_dir="${XDG_DATA_HOME:?}/opencode"
auth_store="$auth_dir/auth.json"
auth_payload=""
case "$auth_source" in
environment)
	IFS= read -r -d '' auth_payload <&3 || {
		printf 'error: could not read isolated environment authentication\n' >&2
		exit 2
	}
	[[ -n "$auth_payload" ]] || {
		printf 'error: isolated environment authentication is empty\n' >&2
		exit 2
	}
	;;
auth_file)
	IFS= read -r -d '' auth_payload <&3 || {
		printf 'error: could not read isolated file authentication\n' >&2
		exit 2
	}
	[[ -n "$auth_payload" ]] || {
		printf 'error: isolated file authentication is empty\n' >&2
		exit 2
	}
	;;
none)
	;;
*)
	printf 'error: unsupported isolated authentication source\n' >&2
	exit 2
	;;
esac
exec 3<&-
if [[ "$auth_source" != none ]]; then
	umask 077
	mkdir -p -- "$auth_dir"
	chmod 700 -- "$auth_dir"
	if [[ "$auth_source" == environment ]]; then
		printf '%s' "$auth_payload" | jq -Rsc '{opencode: {type: "api", key: .}}' >"$auth_store"
	else
		printf '%s' "$auth_payload" >"$auth_store"
	fi
	chmod 600 -- "$auth_store"
fi
auth_payload=""
unset auth_payload OPENCODE_API_KEY OPENCODE_AUTH_CONTENT
exec "$@"
BOOTSTRAP
	chmod 700 "$path"
}

run_isolated_opencode() (
	local tmp_root="$1"
	local wrapper_dir="$2"
	local config_json="$3"
	local auth_source="$4"
	local auth_secret="$5"
	local agora_real="$6"
	local invocations="$7"
	local transcript="$8"
	local model="$9"
	local workdir="${10}"
	local auth_bootstrap="$tmp_root/auth-bootstrap"
	shift 10

	write_auth_bootstrap "$auth_bootstrap"

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
		NO_COLOR=1 \
		AGORA_EVALUATOR_REAL="$agora_real" \
		AGORA_EVALUATOR_WRAPPER="$wrapper_dir/agora" \
		AGORA_EVALUATOR_INVOCATIONS="$invocations" \
		AGORA_EVALUATOR_SHELL_ENV_LOG="$tmp_root/shell-environment.log" \
		AGORA_EVALUATOR_TRANSCRIPT="$transcript" \
		AGORA_EVALUATOR_MODEL="$model" \
		AGORA_EVALUATOR_WORKDIR="$workdir" \
		"$auth_bootstrap" "$auth_source" "$@" \
		3< <(printf '%s\0' "$auth_secret")
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
	local auth_source="${12}"
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
		--arg auth_source "$auth_source" \
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
				source: $auth_source,
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
				shell_environment: ($output + "/shell-environment.log"),
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

write_analysis_boundary() {
	local boundary="$1"
	local output="$2"

	if [[ ! -f "$boundary" ]]; then
		jq -nc '{available: false, valid: false, issues: ["boundary_missing"], provenance: {checkout_revision: null, agora_version: null, opencode_version: null, model: null}}' >"$output"
		return
	fi
	if ! jq -e 'type == "object"' "$boundary" >/dev/null 2>&1; then
		jq -nc '{available: true, valid: false, issues: ["boundary_malformed"], provenance: {checkout_revision: null, agora_version: null, opencode_version: null, model: null}}' >"$output"
		return
	fi

	jq -c '
		def nonempty_string: type == "string" and length > 0;
		{
			available: true,
			issues: [
				if ((.checkout | type) == "object" and (.checkout.root | nonempty_string) and (.checkout.revision | nonempty_string)) then empty else "checkout_revision" end,
				if ((.executables | type) == "object" and (.executables.agora | type) == "object" and (.executables.agora.path | nonempty_string) and (.executables.agora.version | nonempty_string)) then empty else "agora_build" end,
				if ((.executables | type) == "object" and (.executables.opencode | type) == "object" and (.executables.opencode.path | nonempty_string) and (.executables.opencode.version | nonempty_string)) then empty else "opencode_build" end,
				if (.model | nonempty_string) then empty else "model" end,
				if ((.authentication | type) == "object" and (.authentication.provider | nonempty_string) and (.authentication.source == "environment" or .authentication.source == "auth_file") and .authentication.persisted == false) then empty else "authentication_boundary" end,
				if ((.permissions | type) == "object" and .permissions["*"] == "deny" and .permissions.bash == "allow") then empty else "permissions" end,
				if ((.nested_agora | type) == "object" and (.nested_agora.wrapper | nonempty_string) and (.nested_agora.command_gate | nonempty_string) and .nested_agora.dry_run == true and .nested_agora.context_enabled == false and .nested_agora.fixed_output == true and .nested_agora.fixed_model == true) then empty else "nested_wrapper" end
			],
			provenance: {
				checkout_revision: (if (.checkout.revision | type) == "string" then .checkout.revision else null end),
				agora_version: (if (.executables.agora.version | type) == "string" then .executables.agora.version else null end),
				opencode_version: (if (.executables.opencode.version | type) == "string" then .executables.opencode.version else null end),
				model: (if (.model | type) == "string" then .model else null end)
			}
		}
		| .valid = (.issues | length == 0)
	' "$boundary" >"$output"
}

normalize_event_line() {
	local source_index="$1"
	local line="$2"
	local normalized
	local command=""
	local command_qualifies=false

	if ! normalized="$(jq -ce --argjson source_index "$source_index" '
		def object_or_empty: if type == "object" then . else {} end;
		def number_or_null: if type == "number" then . else null end;
		def string_or_null: if type == "string" then . else null end;
		if type != "object" or (.type | type) != "string" then
			error("event record must be an object with a string type")
		else
			(.part | object_or_empty) as $part
			| ($part.state | object_or_empty) as $state
			| ($state.metadata | object_or_empty) as $metadata
			| ($state.input | object_or_empty) as $input
			| ($part.tokens | object_or_empty) as $tokens
			| ($tokens.cache | object_or_empty) as $cache
			| (if .type == "tool_use" then (($part.id | type) == "string" and ($part.id | length) > 0) else true end) as $record_valid
			| {
				source_index: $source_index,
				valid: $record_valid,
				issue: (if .type == "tool_use" and ($record_valid | not) then "missing_part_id" else null end),
				type: .type,
				part_id: (if .type == "tool_use" and $record_valid then $part.id else null end),
				provider_call_id: (if .type == "tool_use" and ($part.callID | type) == "string" then $part.callID else null end),
				identity_source: (
					if .type != "tool_use" then null
					elif $record_valid then "part_id"
					else "missing_part_id"
					end
				),
				tool: (if .type == "tool_use" then ($part.tool | string_or_null) else null end),
				status: (if .type == "tool_use" then ($state.status | string_or_null) else null end),
				exit: (if .type == "tool_use" then ($metadata.exit | number_or_null) else null end),
				command_present: (.type == "tool_use" and ($input.command | type) == "string"),
				command: (if .type == "tool_use" and ($input.command | type) == "string" then $input.command else null end),
				tokens: (
					if .type == "step_finish" then {
						total: ($tokens.total | number_or_null),
						input: ($tokens.input | number_or_null),
						output: ($tokens.output | number_or_null),
						reasoning: ($tokens.reasoning | number_or_null),
						cache: {read: ($cache.read | number_or_null), write: ($cache.write | number_or_null)}
					} else null end
				),
				cost: (if .type == "step_finish" then ($part.cost | number_or_null) else null end)
			}
		end
	' <<<"$line" 2>/dev/null)"; then
		ANALYSIS_EVENTS_VALID=false
		jq -nc --argjson source_index "$source_index" '{source_index: $source_index, valid: false, issue: "malformed_json"}'
		return
	fi

	if [[ "$(jq -r '.type' <<<"$normalized")" == tool_use ]] && [[ "$(jq -r '.command_present' <<<"$normalized")" == true ]]; then
		IFS= read -r -d '' command < <(jq -j '.command, "\u0000"' <<<"$normalized") || true
		if is_qualifying_agora_command "$command"; then
			command_qualifies=true
		fi
	fi

	if [[ "$(jq -r '.valid' <<<"$normalized")" != true ]]; then
		ANALYSIS_EVENTS_VALID=false
	fi

	jq -c --argjson command_qualifies "$command_qualifies" '
		.command_qualifies = $command_qualifies
		| .lifecycle_snapshot = (
			if .type == "tool_use" then {
				tool,
				status,
				exit,
				command_present,
				command_qualifies
			} else null end
		)
		| del(.command)
	' <<<"$normalized"
}

normalize_transcript_line() {
	local source_index="$1"
	local line="$2"

	if ! jq -ce --argjson source_index "$source_index" '
		def object_or_empty: if type == "object" then . else {} end;
		def number_or_null: if type == "number" then . else null end;
		def string_or_null: if type == "string" then . else null end;
		def scalar_or_null: if type == "string" or type == "number" then . else null end;
		if type != "object"
			or (has("transcript") and .transcript != null and (.transcript | type) != "object")
			or (has("turn") and .turn != null and (.turn | type) != "number") then
			error("transcript record has an invalid structure")
		else
			(.transcript | if type == "object" then . else null end) as $metadata
			| (.evidence | if type == "object" then . else null end) as $evidence
			| (.tokens | object_or_empty) as $tokens
			| ($tokens.cache | object_or_empty) as $cache
			| (if $evidence == null then
				{present: false, performed: null, source_reference_count: null}
			  else
				($evidence | has("source_references")) as $has_source_references
				| ($evidence.source_references | type) as $source_references_type
				| (if ($evidence.research_performed | type) == "boolean" then $evidence.research_performed
				   elif ($evidence.performed | type) == "boolean" then $evidence.performed
				   elif $source_references_type == "array" then true
				   elif $has_source_references and $evidence.source_references == null then false
				   else null
				   end) as $performed
				| {
					present: true,
					performed: $performed,
					source_reference_count: (
						if $source_references_type == "array" then ($evidence.source_references | length)
						elif $performed == false then 0
						else null
						end
					)
				}
			  end) as $research_evidence
			| {
				source_index: $source_index,
				valid: true,
				turn: (.turn | number_or_null),
				agent_id: (.agent_id | scalar_or_null),
				model: (.model | string_or_null),
				tokens: {
					total: ($tokens.total | number_or_null),
					input: ($tokens.input | number_or_null),
					output: ($tokens.output | number_or_null),
					reasoning: ($tokens.reasoning | number_or_null),
					cache: {read: ($cache.read | number_or_null), write: ($cache.write | number_or_null)}
				},
				cost: (.cost | number_or_null),
				metadata: (
					if $metadata == null then null else {
						cast: (
							if ($metadata.cast | type) == "array" then [
								$metadata.cast[] | {
									id: (.id | scalar_or_null),
									name: (.name | string_or_null),
									persona: (.persona | string_or_null),
									model: (.provider_model | string_or_null)
								}
							] else null end
						),
						config_agents: (
							if ($metadata.config.agents | type) == "array" then [
								$metadata.config.agents[] | {id: (.id | scalar_or_null), model: (.model | string_or_null)}
							] else null end
						),
						research: (if ($metadata.config.research | type) == "boolean" then $metadata.config.research else null end)
					} end
				),
				research_evidence: $research_evidence
			}
		end
	' <<<"$line" 2>/dev/null; then
		ANALYSIS_TRANSCRIPT_VALID=false
		jq -nc --argjson source_index "$source_index" '{source_index: $source_index, valid: false, issue: "malformed_json"}'
	fi
}

normalize_jsonl_file() {
	local kind="$1"
	local input="$2"
	local output="$3"
	local source_index=0
	local line

	: >"$output"
	if [[ ! -f "$input" ]]; then
		if [[ "$kind" == events ]]; then
			ANALYSIS_EVENTS_STATE=missing
		else
			ANALYSIS_TRANSCRIPT_STATE=missing
		fi
		return
	fi

	while IFS= read -r line || [[ -n "$line" ]]; do
		((source_index += 1))
		if [[ "$kind" == events ]]; then
			normalize_event_line "$source_index" "$line" >>"$output"
		else
			normalize_transcript_line "$source_index" "$line" >>"$output"
		fi
	done <"$input"

	if [[ "$kind" == events ]] && [[ "$ANALYSIS_EVENTS_VALID" != true ]]; then
		ANALYSIS_EVENTS_STATE=malformed
	fi
	if [[ "$kind" == transcript ]] && [[ "$ANALYSIS_TRANSCRIPT_VALID" != true ]]; then
		ANALYSIS_TRANSCRIPT_STATE=malformed
	fi
}

write_analysis_result() {
	local output="$1"
	local analysis_tmp="$2"
	local result="$output/$RESULT_FILENAME"
	local result_tmp="$analysis_tmp/$RESULT_FILENAME"
	local stderr_state=missing
	local invocations_state=missing

	[[ -f "$output/opencode.stderr" ]] && stderr_state=present
	[[ -f "$output/agora-invocations.log" ]] && invocations_state=present

	jq -S -n \
		--arg schema "$RESULT_SCHEMA" \
		--argjson schema_version "$RESULT_SCHEMA_VERSION" \
		--arg events_state "$ANALYSIS_EVENTS_STATE" \
		--arg transcript_state "$ANALYSIS_TRANSCRIPT_STATE" \
		--arg stderr_state "$stderr_state" \
		--arg invocations_state "$invocations_state" \
		--slurpfile boundary "$analysis_tmp/boundary.json" \
		--slurpfile events "$analysis_tmp/events.jsonl" \
		--slurpfile transcript "$analysis_tmp/transcript.jsonl" \
		'
		def sensitive:
			type == "string"
			and test("(?i)(authorization[[:space:]]*[:=]|bearer[[:space:]]+[A-Za-z0-9._-]+|api[_-]?key[[:space:]]*[:=]|access[_-]?token[[:space:]]*[:=]|refresh[_-]?token[[:space:]]*[:=]|password[[:space:]]*[:=]|secret[[:space:]]*[:=]|sk-[A-Za-z0-9_-]{8,}|AKIA[0-9A-Z]{16})");
		def safe_text:
			if type == "string" then (if sensitive then "[redacted]" else . end) else null end;
		def safe_scalar:
			if type == "string" then safe_text elif type == "number" or type == "boolean" then . else null end;
		def ordered_distinct:
			reduce .[] as $value ([]; if index($value) == null then . + [$value] else . end);
		def sum_or_unknown($values; $count):
			if $count == 0 then 0
			elif ($values | all(type == "number")) then ($values | add)
			else null
			end;
		def usage_for($records):
			($records | length) as $count
			| {
				tokens: {
					total: sum_or_unknown([$records[] | .tokens.total]; $count),
					input: sum_or_unknown([$records[] | .tokens.input]; $count),
					output: sum_or_unknown([$records[] | .tokens.output]; $count),
					reasoning: sum_or_unknown([$records[] | .tokens.reasoning]; $count),
					cache: {
						read: sum_or_unknown([$records[] | .tokens.cache.read]; $count),
						write: sum_or_unknown([$records[] | .tokens.cache.write]; $count)
					}
				},
				cost: sum_or_unknown([$records[] | .cost]; $count)
			};
		def qualifying_call($boundary_valid):
			.tool == "bash"
			and .status == "completed"
			and .exit == 0
			and .command_qualifies == true
			and $boundary_valid;
		def safe_cast_member:
			{id: (.id | safe_scalar), name: (.name | safe_text), persona: (.persona | safe_text), model: (.model | safe_text)};
		($boundary[0] // {available: false, valid: false, issues: ["boundary_missing"], provenance: {checkout_revision: null, agora_version: null, opencode_version: null, model: null}}) as $boundary_info
		| ($events_state == "present" and ($events | all(.valid == true))) as $events_valid
		| ($transcript_state == "present" and ($transcript | all(.valid == true))) as $transcript_valid
		| (if $boundary_info.valid == true then true else false end) as $boundary_valid
		| [$events[] | select(.type == "tool_use")] as $tool_records
		| (reduce $tool_records[] as $tool (
			{seen: {}, canonical: [], trace: [], target: null};
			if $tool.valid != true then
				.trace += [$tool + {counted: false, counted_tool_call: null, duplicate_of: null, lifecycle: "invalid"}]
			else
				$tool.part_id as $part_id
				| $tool.lifecycle_snapshot as $snapshot
				| if .seen[$part_id] == null then
					(.canonical | length + 1) as $ordinal
					| .seen[$part_id] = {ordinal: $ordinal, snapshot: $snapshot}
					| .canonical += [$tool + {counted: true, counted_tool_call: $ordinal, duplicate_of: null, lifecycle: "first"}]
					| .trace += [$tool + {counted: true, counted_tool_call: $ordinal, duplicate_of: null, lifecycle: "first"}]
					| if .target == null and ($tool | qualifying_call($boundary_valid)) then
						.target = ($tool + {counted_tool_call: $ordinal})
					  else . end
				  elif .seen[$part_id].snapshot == $snapshot then
					.seen[$part_id].ordinal as $ordinal
					| .trace += [$tool + {counted: false, counted_tool_call: $ordinal, duplicate_of: $ordinal, lifecycle: "duplicate"}]
				  else
					.seen[$part_id].ordinal as $ordinal
					| .seen[$part_id].snapshot = $snapshot
					| .canonical |= map(
						if .counted_tool_call == $ordinal then $tool + {counted: true, counted_tool_call: $ordinal, duplicate_of: null, lifecycle: "first"} else . end
					)
					| .trace += [$tool + {counted: false, counted_tool_call: $ordinal, duplicate_of: null, lifecycle: "update"}]
					| if .target == null and ($tool | qualifying_call($boundary_valid)) then
						.target = ($tool + {counted_tool_call: $ordinal})
					  else . end
				  end
			end
		)) as $calls
		| (if $events_valid and $boundary_valid then $calls.target else null end) as $target
		| [$transcript[] | select(.valid == true and (.metadata | type) == "object")] as $metadata_records
		| (if ($metadata_records | length) > 0 then $metadata_records[0].metadata else null end) as $metadata
		| [$transcript[] | select(.valid == true and (.turn | type) == "number" and .turn >= 0)] as $turns
		| [$events[] | select(.valid == true and .type == "step_finish")] as $outer_steps
		| (if $transcript_valid and $metadata != null and ($metadata.cast | type) == "array" then ($metadata.cast | map(safe_cast_member)) else null end) as $cast_agents
		| (if $transcript_valid then
			[
				(if $metadata != null and ($metadata.cast | type) == "array" then $metadata.cast[] | .model else empty end),
				(if $metadata != null and ($metadata.config_agents | type) == "array" then $metadata.config_agents[] | .model else empty end),
				($turns[] | .model)
			]
			| map(select(type == "string") | safe_text)
			| ordered_distinct
			| if length == 0 then null else . end
		  else null end) as $observed_models
		| (if $transcript_valid then
			[
				(if $metadata != null and ($metadata.cast | type) == "array" then $metadata.cast[] | .id else empty end),
				(if $metadata != null and ($metadata.config_agents | type) == "array" then $metadata.config_agents[] | .id else empty end),
				($turns[] | .agent_id)
			]
			| map(select(type == "string" or type == "number") | safe_scalar)
			| ordered_distinct
			| if length == 0 then null else . end
		  else null end) as $agent_ids
		| (if $transcript_valid and $metadata != null then $metadata.research else null end) as $research_enabled
		| (if $transcript_valid then [$transcript[] | select(.valid == true and .research_evidence.present == true) | .research_evidence] else [] end) as $research_evidence_records
		| (if $transcript_valid and ($research_evidence_records | length) > 0 then
			if any($research_evidence_records[]; .performed == true) then true
			elif any($research_evidence_records[]; .performed == false) then false
			else null
			end
		  else null end) as $research_performed
		| (if $transcript_valid and ($research_evidence_records | length) > 0 and ([$research_evidence_records[] | .source_reference_count] | all(type == "number")) then
			[$research_evidence_records[] | .source_reference_count] | add
		  else null end) as $source_reference_count
		| (if $transcript_valid then usage_for($turns) else {tokens: {total: null, input: null, output: null, reasoning: null, cache: {read: null, write: null}}, cost: null} end) as $agora_usage
		| (if $events_valid then usage_for($outer_steps) else {tokens: {total: null, input: null, output: null, reasoning: null, cache: {read: null, write: null}}, cost: null} end) as $outer_usage
		| [
			$boundary_info.provenance.checkout_revision,
			$boundary_info.provenance.agora_version,
			$boundary_info.provenance.opencode_version,
			$boundary_info.provenance.model,
			(if $metadata != null and ($metadata.cast | type) == "array" then $metadata.cast[] | (.id, .name, .persona, .model) else empty end),
			(if $metadata != null and ($metadata.config_agents | type) == "array" then $metadata.config_agents[] | (.id, .model) else empty end),
			($turns[] | (.agent_id, .model))
		] | map(select(type == "string") | select(sensitive)) | length as $redaction_count
		| ($boundary_valid and $events_valid and $target != null and $transcript_valid and $redaction_count == 0) as $complete
		| {
				schema: $schema,
				schema_version: $schema_version,
				provenance: {
					evaluator: {script: "scripts/eval-cli-discovery.sh", analysis_version: $schema_version},
					build: {checkout_revision: ($boundary_info.provenance.checkout_revision | safe_text), agora_version: ($boundary_info.provenance.agora_version | safe_text)},
					opencode: {version: ($boundary_info.provenance.opencode_version | safe_text), model: ($boundary_info.provenance.model | safe_text)}
				},
				raw_evidence: {
					root: ".",
					boundary: {path: "boundary.json", state: (if $boundary_info.available == true then "present" else "missing" end)},
					events: {path: "events.jsonl", state: $events_state},
					transcript: {path: "agora-transcript.jsonl", state: $transcript_state},
					stderr: {path: "opencode.stderr", state: $stderr_state},
					invocations: {path: "agora-invocations.log", state: $invocations_state}
				},
				summary: {
					score: (if $target == null then null else $target.counted_tool_call end),
					score_unit: "unique structured tool calls through the first completed qualifying Agora wrapper run",
					counted_tool_call_definition: "The first source-ordered valid tool_use record for each OpenCode part.id is counted. Later lifecycle records update that part, exact snapshots are duplicates, and ID-less tool records fail closed.",
					qualification_definition: "A verified boundary plus the first lifecycle record for a counted OpenCode part that is a completed bash event with numeric exit 0 and a command that exactly satisfies the production wrapper run policy.",
					research_performed_definition: "Performed is true when transcript evidence explicitly says so or records a source-reference array, false when transcript evidence explicitly says not performed or records null source references, and null when evidence is absent or inconclusive.",
					counted_tool_calls: (if $events_valid then ($calls.canonical | length) else null end),
					post_score_tool_calls: (if $events_valid and $target != null then (($calls.canonical | length) - $target.counted_tool_call) else null end),
					qualifying_run_found: ($target != null),
					outer_model_steps: (if $events_valid then ($outer_steps | length) else null end),
					complete: $complete
				},
				evidence: {
					model: {requested: ($boundary_info.provenance.model | safe_text), observed: $observed_models},
					cast: {count: (if $cast_agents == null then null else ($cast_agents | length) end), agents: $cast_agents},
					personas: (if $cast_agents == null then null else ($cast_agents | map(.persona)) end),
					agents: $agent_ids,
					research: {enabled: $research_enabled, performed: $research_performed, source_reference_count: $source_reference_count},
					turns: {count: (if $transcript_valid then ($turns | length) else null end)},
					usage: {agora: $agora_usage, outer: $outer_usage}
				},
				checklist: [
					{id: "boundary_integrity", status: (if $boundary_valid then "pass" else "fail" end), value: (if $boundary_valid then "verified" else "unverified" end)},
					{id: "event_stream", status: (if $events_valid then "pass" else "fail" end), value: $events_state},
					{id: "qualifying_run", status: (if $target != null then "pass" else "fail" end), value: (if $target == null then "not_found" else ("tool_call_" + ($target.counted_tool_call | tostring)) end)},
					{id: "transcript", status: (if $transcript_valid then "pass" else "fail" end), value: $transcript_state},
					{id: "model", status: (if $observed_models == null then "unknown" else "pass" end), value: (if $observed_models == null then "unknown" else "observed" end)},
					{id: "cast", status: (if $cast_agents == null then "unknown" else "pass" end), value: (if $cast_agents == null then "unknown" else ($cast_agents | length | tostring) end)},
					{id: "research", status: (if $research_performed == null then "unknown" else "pass" end), value: (if $research_performed == null then "unknown" elif $research_performed then "performed" else "not_performed" end)},
					{id: "usage", status: (if $agora_usage.tokens.total == null or $agora_usage.cost == null then "unknown" else "pass" end), value: (if $agora_usage.tokens.total == null or $agora_usage.cost == null then "unknown" else "known" end)},
					{id: "sensitive_fields", status: (if $redaction_count == 0 then "pass" else "fail" end), value: $redaction_count}
				],
				trace: ($calls.trace | map(
					{
						source_index,
						counted: .counted,
						counted_tool_call,
						duplicate_of,
						lifecycle,
						identity_source,
						tool: (.tool | safe_text),
						status: (.status | safe_text),
						exit,
						qualification: (
							if .valid != true then "missing_part_id"
							elif .lifecycle == "duplicate" then "duplicate"
							elif ($events_valid | not) then "unscored_invalid_event_stream"
							elif $target != null and .source_index == $target.source_index then "first_qualifying_run"
							elif qualifying_call($boundary_valid) then "after_first_qualifying_run"
							elif .command_qualifies == true and .status != "completed" then "incomplete_or_failed"
							elif .command_qualifies == true then "nonzero_or_missing_exit"
							else "not_qualifying"
							end
						)
					}
				))
			}
		' >"$result_tmp"
	mv -- "$result_tmp" "$result"
	jq -e '.summary.complete == true' "$result" >/dev/null
}

analyze_trial_output() (
	local output="$1"
	local analysis_tmp
	local status=0

	[[ -d "$output" ]] || {
		printf 'error: analysis output directory does not exist: %s\n' "$output" >&2
		return 2
	}
	command -v jq >/dev/null 2>&1 || {
		printf 'error: jq is required for deterministic analysis\n' >&2
		return 2
	}

	analysis_tmp="$(mktemp -d "${TMPDIR:-/tmp}/agora-cli-analysis.XXXXXX")"
	trap 'rm -rf -- "$analysis_tmp"' EXIT
	ANALYSIS_EVENTS_STATE=present
	ANALYSIS_TRANSCRIPT_STATE=present
	ANALYSIS_EVENTS_VALID=true
	ANALYSIS_TRANSCRIPT_VALID=true
	write_analysis_boundary "$output/boundary.json" "$analysis_tmp/boundary.json"
	normalize_jsonl_file events "$output/events.jsonl" "$analysis_tmp/events.jsonl"
	normalize_jsonl_file transcript "$output/agora-transcript.jsonl" "$analysis_tmp/transcript.jsonl"
	if write_analysis_result "$output" "$analysis_tmp"; then
		:
	else
		status=$?
		if [[ -f "$output/$RESULT_FILENAME" ]]; then
			printf 'error: analysis did not produce a complete qualifying result\n' >&2
		fi
	fi
	exit "$status"
)

run_trial() {
	local output="$1"
	local model="$2"
	local timeout_duration="$3"
	local opencode_request="$4"
	local auth_file="$5"
	local parent
	local opencode_bin
	local opencode_version
	local auth_source=""
	local auth_secret=""
	local credential_values=""
	local tmp_root=""
	local agora_bin
	local agora_version
	local wrapper
	local shell_gate
	local auth_bootstrap
	local workdir
	local config_json
	local outer_status
	local outer_pid=""
	local output_created=0
	local remove_output=0
	local cleanup_status=0
	local errexit_enabled=0
	[[ "$-" == *e* ]] && errexit_enabled=1

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

	# shellcheck disable=SC2329 # Called directly and through the EXIT trap below.
	cleanup() {
		local status="$1"

		trap - EXIT HUP INT TERM
		terminate_outer_process_group
		if ((status != 0)); then
			remove_output=1
		fi
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
		auth_secret=''
		credential_values=''
		SELECTED_AUTH_SECRET=''
		SELECTED_CREDENTIAL_VALUES=''
		unset auth_secret credential_values SELECTED_AUTH_SECRET SELECTED_CREDENTIAL_VALUES
		if [[ -n "$tmp_root" ]] && ! rm -rf -- "$tmp_root"; then
			printf 'error: could not remove temporary evaluator state\n' >&2
			if ((status == 0)); then
				status=2
			fi
		fi
		cleanup_status="$status"
	}

	# shellcheck disable=SC2329 # Called through the EXIT trap below.
	cleanup_on_exit() {
		local status=$?

		cleanup "$status"
		exit "$cleanup_status"
	}

	# shellcheck disable=SC2329 # Called through the signal traps below.
	interrupt_trial() {
		local status="$1"
		local signal="$2"

		remove_output=1
		trap - HUP INT TERM
		terminate_outer_process_group "$signal"
		cleanup "$status"
		exit "$cleanup_status"
	}

	trap cleanup_on_exit EXIT
	trap 'interrupt_trial 129 HUP' HUP
	trap 'interrupt_trial 130 INT' INT
	trap 'interrupt_trial 143 TERM' TERM

	for command in go jq timeout grep setsid; do
		command -v "$command" >/dev/null 2>&1 || die "$command is required"
	done

	opencode_bin="$(resolve_executable "$opencode_request")" || die "opencode executable is required: $opencode_request"
	opencode_version="$("$opencode_bin" --version)"
	select_auth_for_model "$auth_file" "$model" || die "no supported authentication is available for $(provider_for_model "$model" 2>/dev/null || printf 'the selected') provider"
	auth_source="$SELECTED_AUTH_SOURCE"
	auth_secret="$SELECTED_AUTH_SECRET"
	credential_values="$SELECTED_CREDENTIAL_VALUES"

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
	auth_bootstrap="$tmp_root/bin/opencode-auth-bootstrap"
	write_auth_bootstrap "$auth_bootstrap"
	workdir="$tmp_root/work"
	config_json="$(opencode_permission_config "$shell_gate")"

	write_boundary "$output/boundary.json" "$tmp_root" "$output" "$model" "$opencode_bin" "$opencode_version" "$agora_bin" "$agora_version" "$wrapper" "$workdir" "$shell_gate" "$auth_source"
	: >"$output/agora-invocations.log"

	set +e
	# Start setsid here so outer_pid is the child process-group leader.
	setsid --wait \
		timeout "$timeout_duration" \
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
			NO_COLOR=1 \
			AGORA_EVALUATOR_REAL="$agora_bin" \
			AGORA_EVALUATOR_WRAPPER="$wrapper" \
			AGORA_EVALUATOR_INVOCATIONS="$output/agora-invocations.log" \
			AGORA_EVALUATOR_SHELL_ENV_LOG="$output/shell-environment.log" \
			AGORA_EVALUATOR_TRANSCRIPT="$output/agora-transcript.jsonl" \
			AGORA_EVALUATOR_MODEL="$model" \
			AGORA_EVALUATOR_WORKDIR="$workdir" \
			"$auth_bootstrap" "$auth_source" "$opencode_bin" run \
			--pure \
			--format json \
			--model "$model" \
			--agent "$EVALUATOR_AGENT" \
			--title "Agora CLI discovery evaluation" \
			--dir "$workdir" \
			"$PROMPT" \
			3< <(printf '%s\0' "$auth_secret") \
			>"$output/events.jsonl" 2>"$output/opencode.stderr" &
	outer_pid=$!
	wait "$outer_pid"
	outer_status=$?
	if ((errexit_enabled)); then
		set -e
	else
		set +e
	fi

	trap - EXIT HUP INT TERM
	cleanup "$outer_status"
	return "$cleanup_status"
}

main() {
	local model="$DEFAULT_MODEL"
	local output=""
	local analyze_output=""
	local timeout_duration="5m"
	local host_data_home="${XDG_DATA_HOME:-${HOME:?}/.local/share}"
	local auth_file="$host_data_home/opencode/auth.json"
	local opencode_request="${AGORA_EVALUATOR_OPENCODE_BIN:-opencode}"
	local self_test=""
	local self_test_count=0
	local non_test_options=0
	local trial_only_options=0
	local quiet=0
	local trial_status=0
	local analysis_status=0

	while (($# > 0)); do
		case "$1" in
		--output)
			require_value "$1" "${2:-}"
			output="$2"
			non_test_options=$((non_test_options + 1))
			shift 2
			;;
		--analyze)
			require_value "$1" "${2:-}"
			analyze_output="$2"
			non_test_options=$((non_test_options + 1))
			shift 2
			;;
		--model)
			require_value "$1" "${2:-}"
			model="$2"
			non_test_options=$((non_test_options + 1))
			trial_only_options=$((trial_only_options + 1))
			shift 2
			;;
		--auth-file)
			require_value "$1" "${2:-}"
			auth_file="$2"
			non_test_options=$((non_test_options + 1))
			trial_only_options=$((trial_only_options + 1))
			shift 2
			;;
		--timeout)
			require_value "$1" "${2:-}"
			timeout_duration="$2"
			non_test_options=$((non_test_options + 1))
			trial_only_options=$((trial_only_options + 1))
			shift 2
			;;
		--self-test)
			self_test=boundary
			self_test_count=$((self_test_count + 1))
			shift
			;;
		--analysis-self-test)
			self_test=analysis
			self_test_count=$((self_test_count + 1))
			shift
			;;
		--quiet)
			quiet=1
			non_test_options=$((non_test_options + 1))
			shift
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

	if [[ -n "$self_test" ]]; then
		((self_test_count == 1 && non_test_options == 0)) || die "--self-test cannot be combined with trial or analysis options"
		if [[ "$self_test" == analysis ]]; then
			exec "$ROOT/scripts/eval-cli-discovery-test.sh" --analysis-only
		fi
		exec "$ROOT/scripts/eval-cli-discovery-test.sh"
	fi

	if [[ -n "$analyze_output" ]]; then
		if [[ -n "$output" ]] || ((trial_only_options != 0)); then
			die "--analyze cannot be combined with trial options"
		fi
		if [[ "$analyze_output" != /* ]]; then
			analyze_output="$ROOT/$analyze_output"
		fi
		if analyze_trial_output "$analyze_output"; then
			:
		else
			analysis_status=$?
			return "$analysis_status"
		fi
		if ((quiet)); then
			printf '%s\n' "$analyze_output/$RESULT_FILENAME"
		else
			printf 'Result: %s\n' "$analyze_output/$RESULT_FILENAME"
		fi
		return 0
	fi

	[[ -n "$output" ]] || die "--output is required"
	if [[ "$output" != /* ]]; then
		output="$ROOT/$output"
	fi
	set +e
	run_trial "$output" "$model" "$timeout_duration" "$opencode_request" "$auth_file"
	trial_status=$?
	set -e
	if [[ -d "$output" ]]; then
		set +e
		analyze_trial_output "$output"
		analysis_status=$?
		set -e
	fi
	if ((trial_status != 0)); then
		return "$trial_status"
	fi
	if ((analysis_status != 0)); then
		return "$analysis_status"
	fi
	if ((quiet)); then
		printf '%s\n' "$output/$RESULT_FILENAME"
	else
		printf 'Result: %s\n' "$output/$RESULT_FILENAME"
	fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
