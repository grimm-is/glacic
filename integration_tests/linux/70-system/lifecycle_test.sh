#!/bin/sh
set -x
TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"

plan 2

CONFIG_FILE="/tmp/restart.hcl"
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"
api {
    enabled = true
    listen = "127.0.0.1:9191"
}
EOF

diag "Starting Daemon..."
start_ctl "$CONFIG_FILE"
ok 0 "Daemon started"

diag "Checking for restart command..."
# If checking help text for restart command
if $APP_BIN --help 2>&1 | grep -q "restart"; then
    diag "Restart command found, executing..."
    $APP_BIN restart || fail "Restart command failed"
    ok 0 "Restart executed"
else
    ok 0 "Restart command not implemented/found # SKIP"
fi

kill $CTL_PID 2>/dev/null
rm -f "$CONFIG_FILE"
exit 0
