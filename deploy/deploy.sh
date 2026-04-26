#!/usr/bin/env bash
#
# Build + deploy the digest binary to a remote Ubuntu server.
#
# Usage:
#   ./deploy/deploy.sh                        # uses DEPLOY_HOST from deploy.env
#   ./deploy/deploy.sh <host>                 # explicit (overrides env)
#   HOST=host ./deploy/deploy.sh              # env var override
#   SKIP_TESTS=1 ./deploy/deploy.sh           # emergency bypass
#
# By default runs `make test-race` before build; deploy aborts on test
# failure. The server must already be bootstrapped via deploy/install.sh.
# Timer-driven services pick up the new binary on the next fire — no
# restart needed. Uncommitted changes get embedded into the version
# string as `-dirty` via the Makefile's git-describe.

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1

LOCAL_BIN="bin/digest-linux-amd64"
REMOTE_BIN="/usr/local/bin/digest"

if [[ ${SKIP_TESTS:-0} == 1 ]]; then
  echo "==> SKIP_TESTS=1 — skipping test suite"
else
  echo "==> Running test suite (make test-race)"
  make test-race
fi

echo "==> Building $LOCAL_BIN"
make build-linux-amd64

echo "==> Preserving current $REMOTE_BIN as ${REMOTE_BIN}.prev (if present)"
ssh "$HOST" "if [[ -x $REMOTE_BIN ]]; then cp -p $REMOTE_BIN ${REMOTE_BIN}.prev; fi"

echo "==> Copying to $HOST:$REMOTE_BIN"
scp "$LOCAL_BIN" "$HOST:$REMOTE_BIN"

echo "==> chmod + version check"
ssh "$HOST" "chmod 755 $REMOTE_BIN && $REMOTE_BIN -version"

cat <<EOF

Deploy complete. Next scheduled fire picks up the new binary.
To force an immediate run:
  ssh $HOST 'systemctl start it-digest-watch.service'
  ssh $HOST 'systemctl start it-digest-daily.service'
EOF
