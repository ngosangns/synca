#!/bin/bash
# Stub of the synca CLI for feature tests.
# Records every invocation (one line: "$@") to $SYNCA_STUB_LOG when set.

if [ -n "$SYNCA_STUB_LOG" ]; then
    printf '%s\n' "$*" >> "$SYNCA_STUB_LOG"
fi

if [ "$1" = "--version" ]; then
    echo "synca 0.0.0-test"
    exit 0
fi

for arg in "$@"; do
    if [ "$arg" = "--dry-run" ]; then
        echo "Sync plan (scope=user, actions=2):"
        echo '  1. {"kind":"skip_same","path":"/tmp/x","skill_key":"demo","reason":"identical"}'
        echo '  2. {"kind":"conflict_skill","skill_key":"demo-conflict","paths":["/a","/b"],"hashes":["h1","h2"]}'
        echo "(dry-run; no changes)"
        exit 0
    fi
done

case "$1 $2" in
    "skills list")
        echo '[{"key":"demo","display_name":"demo","description":"A demo skill.","scope":"user","presence":[{"agent":"agents","path":"/home/u/.agents/skills/demo","is_symlink":false,"symlink_target":null,"content_hash":"abc123def456"}],"mismatch":false},{"key":"demo-conflict","display_name":"demo-conflict","description":null,"scope":"user","presence":[{"agent":"agents","path":"/home/u/.agents/skills/demo-conflict","is_symlink":false,"symlink_target":null,"content_hash":"aaa"},{"agent":"devin","path":"/home/u/.config/devin/skills/demo-conflict","is_symlink":false,"symlink_target":null,"content_hash":"bbb"}],"mismatch":true}]'
        ;;
    "mcp list")
        echo '[{"key":"demo-mcp","scope":"user","presence":[{"agent":"agents","path":"/home/u/.agents/mcp.json","normalized":{"transport":"http","command":null,"url":"http://127.0.0.1:9010/mcp","args":null,"enabled":true,"env_keys":[]},"fingerprint":"fp123"}],"mismatch":false}]'
        ;;
    "update --check")
        echo '{"ok":true,"current":"0.0.0","latest":"0.0.1","update_available":true,"message":"update available"}'
        ;;
    "update --json"|"update")
        echo '{"ok":true,"current":"0.0.0","latest":"0.0.1","update_available":false,"message":"updated to 0.0.1"}'
        ;;
    *)
        echo "applied: $*"
        ;;
esac
exit 0
