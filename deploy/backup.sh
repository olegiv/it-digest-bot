#!/usr/bin/env bash
#
# Pull a consistent SQLite snapshot of state.db from the server.
#
# Uses sqlite3's .backup command on the server (WAL-aware, locks the
# DB briefly, safe while services run) and scp's the result into a
# local backups/ dir with an ISO-8601 UTC timestamp.
#
# Requires sqlite3 on the server (`apt-get install sqlite3` on Ubuntu).
#
# Usage:
#   ./deploy/backup.sh                  # uses DEPLOY_HOST from deploy.env
#   ./deploy/backup.sh <host>           # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1

LOCAL_DIR="backups"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
REMOTE_TMP="/tmp/it-digest-state-$STAMP.db"
LOCAL_FILE="$LOCAL_DIR/state-$STAMP.db"
REMOTE_DB="/var/lib/it-digest/state.db"

mkdir -p "$LOCAL_DIR"

echo "==> Snapshotting $REMOTE_DB via sqlite3 .backup"
ssh "$HOST" "sudo -u it-digest sqlite3 '$REMOTE_DB' \".backup '$REMOTE_TMP'\""

echo "==> Copying to $LOCAL_FILE"
scp "$HOST:$REMOTE_TMP" "$LOCAL_FILE"

echo "==> Removing remote temp"
ssh "$HOST" "rm -f '$REMOTE_TMP'"

echo ""
echo "Backup saved:"
ls -lh "$LOCAL_FILE"
