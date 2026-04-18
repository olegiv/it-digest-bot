#!/usr/bin/env bash
#
# Swap /usr/local/bin/digest with the .prev backup left by deploy.sh.
# Intended for reverting a broken deploy without re-building from source.
#
# After rollback:
#   /usr/local/bin/digest        — the prior binary (now active)
#   /usr/local/bin/digest.failed — the broken one (kept for inspection)
#
# Usage:
#   ./deploy/rollback.sh                # uses DEPLOY_HOST from deploy.env
#   ./deploy/rollback.sh <host>         # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1

REMOTE_BIN=/usr/local/bin/digest

ssh "$HOST" bash <<REMOTE
set -euo pipefail
if [[ ! -x ${REMOTE_BIN}.prev ]]; then
  echo 'error: ${REMOTE_BIN}.prev does not exist — nothing to roll back to' >&2
  echo '  (deploy.sh only creates .prev when an existing binary is present)' >&2
  exit 1
fi
mv ${REMOTE_BIN} ${REMOTE_BIN}.failed
mv ${REMOTE_BIN}.prev ${REMOTE_BIN}
echo 'rolled back:'
${REMOTE_BIN} version
REMOTE

cat <<EOF

The prior binary is now active. The failed one is preserved at
${REMOTE_BIN}.failed on $HOST for inspection. Run 'make deploy'
again with a fixed source to replace it.
EOF
