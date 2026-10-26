#!/bin/sh
# Create a hook script directory. Never adds or trusts a hook.
set -eu

usage() {
	cat <<'EOF'
usage: hook-scaffold.sh <id> [--event Event] [--tools shell,read] [--force]

Creates ~/.agents/hooks/<id>/check.sh (mode 0755) and prints the exact
"synca hooks add" and "synca hooks trust" commands. It does not edit the hub,
does not sync, and never approves anything.

--force overwrites an existing check.sh. Nothing runs the script.
EOF
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
raw=$1
shift
case $raw in
-h | --help) usage; exit 0 ;;
esac

id=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]' | tr ' _' '--')
bad_id() {
	echo "hook-scaffold.sh: '$raw' -> '$id' is not a valid hook id (lowercase letters, digits and '-', max 64)" >&2
	exit 2
}
case $id in
[a-z0-9]*) ;;
*) bad_id ;;
esac
case $id in
*[!a-z0-9-]*) bad_id ;;
esac
[ "${#id}" -le 64 ] || bad_id

event=PreToolUse
tools=
tools_set=0
force=0
while [ $# -gt 0 ]; do
	case $1 in
	--event)
		[ $# -ge 2 ] || { echo "hook-scaffold.sh: --event needs a value" >&2; exit 2; }
		event=$2
		shift 2
		;;
	--event=*) event=${1#*=}; shift ;;
	--tools)
		[ $# -ge 2 ] || { echo "hook-scaffold.sh: --tools needs a value" >&2; exit 2; }
		tools=$2
		tools_set=1
		shift 2
		;;
	--tools=*) tools=${1#*=}; tools_set=1; shift ;;
	--force) force=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) echo "hook-scaffold.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done

tool_event=0
case $event in
PreToolUse | PostToolUse | PostToolUseFailure | PermissionRequest) tool_event=1 ;;
SessionStart | UserPromptSubmit | Stop | SessionEnd | PreCompact | PostCompact | Notification | SubagentStart | SubagentStop) ;;
*)
	echo "hook-scaffold.sh: unknown event '$event' (see: synca hooks --help)" >&2
	exit 2
	;;
esac

if [ "$tool_event" -eq 1 ]; then
	if [ "$tools_set" -eq 0 ]; then
		tools=shell
	elif [ -z "$tools" ]; then
		echo "hook-scaffold.sh: --tools is empty; name at least one class so the hook is not widened to every tool" >&2
		exit 2
	fi
	old_ifs=$IFS
	IFS=,
	for class in $tools; do
		case $class in
		shell | read | write | search | web | agent | mcp) ;;
		*)
			IFS=$old_ifs
			echo "hook-scaffold.sh: unknown tool class '$class' (shell|read|write|search|web|agent|mcp)" >&2
			exit 2
			;;
		esac
	done
	IFS=$old_ifs
elif [ "$tools_set" -eq 1 ]; then
	echo "hook-scaffold.sh: --tools only applies to PreToolUse, PostToolUse, PostToolUseFailure and PermissionRequest" >&2
	exit 2
fi

[ -n "${HOME:-}" ] || { echo "hook-scaffold.sh: HOME is not set" >&2; exit 3; }
hook_dir=$HOME/.agents/hooks/$id
script=$hook_dir/check.sh
mkdir -p "$hook_dir"
if [ -e "$script" ] && [ "$force" -ne 1 ]; then
	echo "hook-scaffold.sh: $script exists (pass --force to overwrite)" >&2
	exit 1
fi
cat >"$script" <<'EOF'
#!/bin/sh
# Hook scaffold. The agent sends its event as JSON on stdin.
# Exit 0 to allow, non-zero to report a failure; exit 2 to block where supported.
set -eu
payload=$(cat)
printf '%s' "$payload" >/dev/null
# TODO: inspect the payload and decide.
exit 0
EOF
chmod 755 "$script"

printf 'created %s\n\n' "$script"
printf 'Next:\n'
if [ -n "$tools" ]; then
	printf "  synca hooks add %s --event %s --tools %s --command '{{hook_dir}}/check.sh' --dry-run\n" "$id" "$event" "$tools"
	printf "  synca hooks add %s --event %s --tools %s --command '{{hook_dir}}/check.sh'\n" "$id" "$event" "$tools"
else
	printf "  synca hooks add %s --event %s --command '{{hook_dir}}/check.sh' --dry-run\n" "$id" "$event"
	printf "  synca hooks add %s --event %s --command '{{hook_dir}}/check.sh'\n" "$id" "$event"
fi
printf '  synca hooks trust %s      # a person types this in a terminal; an agent must not\n' "$id"
printf '  synca hooks sync --dry-run\n'
