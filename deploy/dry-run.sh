#!/usr/bin/env bash
#
# Run `digest <watch|daily> --dry-run` on the server with the systemd
# env file loaded, so secrets are picked up the same way the timers do
# it. Output is streamed back over the SSH session. No Telegram posts,
# no DB writes.
#
# Usage:
#   ./deploy/dry-run.sh <watch|daily>          # uses DEPLOY_HOST
#   ./deploy/dry-run.sh <watch|daily> <host>   # explicit host

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# New signature: <cmd> is always first, [host] is optional.
# Falls back to DEPLOY_HOST / deploy.env if [host] isn't given.
CMD="${1:-}"
if [[ $CMD != watch && $CMD != daily ]]; then
  echo "usage: $0 <watch|daily> [host]" >&2
  exit 1
fi
HOST=$(resolve_host "${2:-}") || exit 1

echo "==> Running 'digest $CMD --dry-run' on $HOST"
ssh "$HOST" "systemd-run \
  --uid=it-digest --gid=it-digest \
  --wait --collect --pipe \
  --property=EnvironmentFile=/etc/it-digest/env \
  --property=Environment=TZ=Europe/Zurich \
  --property=WorkingDirectory=/var/lib/it-digest \
  /usr/local/bin/digest $CMD --dry-run --config /etc/it-digest/config.toml"
