#!/bin/sh
# Read-only synca health summary. Exit 0 when doctor is clean, 8 on drift.
set -eu

usage() {
	cat <<'EOF'
usage: status.sh [--scope user|project] [--cwd DIR] [--json]

Read-only. Prints the synca version, skill and MCP counts, doctor findings,
and hook trust/pending state. Nothing is written.

Exit: 0 clean, 8 doctor drift, 3 synca, a path, or jq is unavailable.
EOF
}

synca_bin=${SYNCA_BIN:-}
if [ -z "$synca_bin" ]; then
	if command -v synca >/dev/null 2>&1; then
		synca_bin=$(command -v synca)
	elif [ -x "${HOME:-}/.local/bin/synca" ]; then
		synca_bin=$HOME/.local/bin/synca
	else
		echo "status.sh: synca not found (set SYNCA_BIN)" >&2
		exit 3
	fi
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "status.sh: jq is required" >&2
	exit 3
fi

scope=user
cwd=.
json_out=0
while [ $# -gt 0 ]; do
	case $1 in
	--scope)
		[ $# -ge 2 ] || { echo "status.sh: --scope needs a value" >&2; exit 2; }
		scope=$2
		shift 2
		;;
	--scope=*) scope=${1#*=}; shift ;;
	--cwd)
		[ $# -ge 2 ] || { echo "status.sh: --cwd needs a value" >&2; exit 2; }
		cwd=$2
		shift 2
		;;
	--cwd=*) cwd=${1#*=}; shift ;;
	--json) json_out=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) echo "status.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
case $scope in
user | project) ;;
*) echo "status.sh: --scope must be user or project" >&2; exit 2 ;;
esac

fail_json() {
	if printf '%s' "$1" | jq -e '.error.message' >/dev/null 2>&1; then
		msg=$(printf '%s' "$1" | jq -r '.error.message')
		hint=$(printf '%s' "$1" | jq -r '.error.hint // empty')
		printf 'status.sh: %s\n' "$msg" >&2
		[ -z "$hint" ] || printf 'hint: %s\n' "$hint" >&2
	else
		[ -z "$1" ] || printf '%s\n' "$1" >&2
	fi
	exit "$2"
}

version_json=$("$synca_bin" version --json) || fail_json "$version_json" 5
skills_json=$("$synca_bin" skills list --scope "$scope" --cwd "$cwd" --json) && skills_rc=0 || skills_rc=$?
[ "$skills_rc" -eq 0 ] || fail_json "$skills_json" "$skills_rc"
mcp_json=$("$synca_bin" mcp list --scope "$scope" --cwd "$cwd" --json) && mcp_rc=0 || mcp_rc=$?
[ "$mcp_rc" -eq 0 ] || fail_json "$mcp_json" "$mcp_rc"
doctor_json=$("$synca_bin" doctor --scope "$scope" --cwd "$cwd" --json) && doctor_rc=0 || doctor_rc=$?
if [ "$doctor_rc" -ne 0 ] && [ "$doctor_rc" -ne 8 ]; then
	fail_json "$doctor_json" "$doctor_rc"
fi

hooks_json='null'
if [ "$scope" = user ]; then
	hooks_json=$("$synca_bin" hooks list --json) && hooks_rc=0 || hooks_rc=$?
	[ "$hooks_rc" -eq 0 ] || fail_json "$hooks_json" "$hooks_rc"
fi

if [ "$json_out" -eq 1 ]; then
	jq -n \
		--argjson version "$version_json" \
		--argjson skills "$skills_json" \
		--argjson mcp "$mcp_json" \
		--argjson doctor "$doctor_json" \
		--argjson hooks "$hooks_json" \
		--arg scope "$scope" \
		--arg cwd "$cwd" '
		{
			version: $version,
			scope: $scope,
			cwd: $cwd,
			skills: {
				total: ($skills | length),
				mismatched: ([$skills[] | select(.mismatch)] | length)
			},
			mcp: {
				total: ($mcp | length),
				mismatched: ([$mcp[] | select(.mismatch)] | length)
			},
			doctor: $doctor,
			hooks: (if $hooks == null then null else {
				total: ($hooks.hooks | length),
				untrusted: ([$hooks.hooks[] | select(.trusted | not)] | length),
				states: ([$hooks.hooks[].agents[].state]
					| group_by(.) | map({state: .[0], count: length}))
			} end)
		}'
else
	jq -n -r \
		--argjson version "$version_json" \
		--argjson skills "$skills_json" \
		--argjson mcp "$mcp_json" \
		--argjson doctor "$doctor_json" \
		--argjson hooks "$hooks_json" \
		--arg scope "$scope" \
		--arg cwd "$cwd" '
		"synca \($version.version) (schema \($version.schemaVersion), commit \($version.commit[0:7])\(if $version.dirty then "*" else "" end))",
		"scope \($scope)  cwd \($cwd)",
		"skills  \($skills | length) (\([$skills[] | select(.mismatch)] | length) mismatched)",
		"mcp     \($mcp | length) (\([$mcp[] | select(.mismatch)] | length) mismatched)",
		(if $doctor.ok then
			"doctor  clean"
		else
			"doctor  \([$doctor.drift[]] | length) drift, \([$doctor.dangling[]] | length) dangling, \([$doctor.missing[]] | length) missing, \([$doctor.mcp[]] | length) mcp"
		end),
		(if $hooks == null then
			"hooks   not checked (user scope only)"
		else
			([$hooks.hooks[].agents[].state] | group_by(.) | map("\(length) \(.[0])")) as $states
			| "hooks   \($hooks.hooks | length) total, \([ $hooks.hooks[] | select(.trusted | not)] | length) not approved" +
				(if ($states | length) > 0 then "; " + ($states | join(", ")) else "" end)
		end)'
fi

[ "$doctor_rc" -eq 0 ] || exit 8
