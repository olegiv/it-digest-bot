#!/usr/bin/env bash
#
# One-call status snapshot: timer schedule + recent journal lines for
# the watch and daily services.
#
# Usage:
#   ./deploy/status.sh                  # uses DEPLOY_HOST from deploy.env
#   ./deploy/status.sh <host>           # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1

echo "==> Timers on $HOST"
ssh "$HOST" 'systemctl list-timers --no-pager "it-digest-*"'

echo ""
echo "==> Binary version"
ssh "$HOST" '/usr/local/bin/digest -version'

echo ""
echo "==> Recent journal (last 20 lines, both services)"
ssh "$HOST" 'journalctl --no-pager -n 20 -u it-digest-watch.service -u it-digest-daily.service'
