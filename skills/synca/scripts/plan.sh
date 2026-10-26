#!/bin/sh
# Read-only dry-run plan summary. Never applies a sync.
set -eu

usage() {
	cat <<'EOF'
usage: plan.sh [skills|mcp|hooks|all] [--scope user|project] [--cwd DIR]
               [--key NAME] [--agents a,b] [--json]

Read-only: runs "synca sync <target> --dry-run --json" and prints a summary.
--key works with skills and mcp only. --json prints the raw plan instead.
EOF
}

synca_bin=${SYNCA_BIN:-}
if [ -z "$synca_bin" ]; then
	if command -v synca >/dev/null 2>&1; then
		synca_bin=$(command -v synca)
	elif [ -x "${HOME:-}/.local/bin/synca" ]; then
		synca_bin=$HOME/.local/bin/synca
	else
		echo "plan.sh: synca not found (set SYNCA_BIN)" >&2
		exit 3
	fi
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "plan.sh: jq is required" >&2
	exit 3
fi

target=all
scope=user
cwd=.
key=
agents=
json_out=0
while [ $# -gt 0 ]; do
	case $1 in
	skills | mcp | hooks | all) target=$1; shift ;;
	--scope)
		[ $# -ge 2 ] || { echo "plan.sh: --scope needs a value" >&2; exit 2; }
		scope=$2
		shift 2
		;;
	--scope=*) scope=${1#*=}; shift ;;
	--cwd)
		[ $# -ge 2 ] || { echo "plan.sh: --cwd needs a value" >&2; exit 2; }
		cwd=$2
		shift 2
		;;
	--cwd=*) cwd=${1#*=}; shift ;;
	--key)
		[ $# -ge 2 ] || { echo "plan.sh: --key needs a value" >&2; exit 2; }
		key=$2
		shift 2
		;;
	--key=*) key=${1#*=}; shift ;;
	--agents)
		[ $# -ge 2 ] || { echo "plan.sh: --agents needs a value" >&2; exit 2; }
		agents=$2
		shift 2
		;;
	--agents=*) agents=${1#*=}; shift ;;
	--json) json_out=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) echo "plan.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
case $scope in
user | project) ;;
*) echo "plan.sh: --scope must be user or project" >&2; exit 2 ;;
esac
case $target in
all | hooks)
	[ -z "$key" ] || { echo "plan.sh: --key does not apply to sync $target" >&2; exit 2; }
	;;
esac

set -- "$target" --scope "$scope" --cwd "$cwd" --dry-run --json
[ -z "$key" ] || set -- "$@" --key "$key"
[ -z "$agents" ] || set -- "$@" --agents "$agents"
out=$("$synca_bin" sync "$@") && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
	if printf '%s' "$out" | jq -e '.error.message' >/dev/null 2>&1; then
		msg=$(printf '%s' "$out" | jq -r '.error.message')
		hint=$(printf '%s' "$out" | jq -r '.error.hint // empty')
		printf 'plan.sh: %s\n' "$msg" >&2
		[ -z "$hint" ] || printf 'hint: %s\n' "$hint" >&2
	else
		[ -z "$out" ] || printf '%s\n' "$out" >&2
	fi
	exit "$rc"
fi

if [ "$json_out" -eq 1 ]; then
	printf '%s\n' "$out"
	exit 0
fi

printf '%s' "$out" | jq -r --arg target "$target" '
	def kinds: ([.[].kind] | group_by(.) | map("\(.[0]) \(length)") | join(", "));
	([
		"plan: \($target) scope=\(.scope) on-conflict=\(.onConflict) dry-run=\(.dryRun)",
		("actions \(.actions | length)" +
			(if (.actions | length) > 0 then ": " + (.actions | kinds) else "" end)),
		(if has("conflicts") then
			("conflicts \(.conflicts | length)" +
				(if (.conflicts | length) > 0 then
					": " + ([.conflicts[] | "\(.key) (\(if .skill then "skill" else "mcp" end))"] | join(", "))
				else "" end))
		else empty end),
		(if has("untrusted") then "untrusted hooks \(.untrusted)" else empty end),
		(if has("hooks") then
			"hooks \(.hooks.actions | length) action(s), \(.hooks.untrusted) not approved"
		else empty end),
		(([(.actions[]? | .lossy[]?)] + [(.hooks.actions[]? | .lossy[]?)])
			| group_by(.code) | map("\(.[0].code) \(length)") | join(", ")) as $warn
			| if ($warn | length) > 0 then "warnings: \($warn)" else empty end
	])[]'
