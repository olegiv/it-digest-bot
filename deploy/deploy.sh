#!/usr/bin/env bash
#
# Build + deploy the digest binary AND systemd unit files to a remote
# Ubuntu server.
#
# Usage:
#   ./deploy/deploy.sh                        # uses DEPLOY_HOST from deploy.env
#   ./deploy/deploy.sh <host>                 # explicit (overrides env)
#   HOST=host ./deploy/deploy.sh              # env var override
#   SKIP_TESTS=1 ./deploy/deploy.sh           # emergency bypass
#
# By default runs `make test-race` before build; deploy aborts on test
# failure. The server must already be bootstrapped via deploy/install.sh.
# After the binary, this script rsyncs deploy/systemd/ to /etc/systemd/
# system/ and `daemon-reload`s if any unit file changed; timers whose
# files changed are restarted so the new schedule takes effect. Timer-
# driven services pick up the new binary on the next fire — no restart
# needed. Uncommitted changes get embedded into the version string as
# `-dirty` via the Makefile's git-describe.

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

echo "==> Syncing systemd unit files to $HOST:/etc/systemd/system/"
# -rlpt (no -ogD): don't try to preserve local owner/group (which is the
# dev user, not root) — rsync-as-root will write files owned by root.
# --checksum: compare by content, so mtime drift after `git checkout`
# does not cause spurious transfers + reloads.
RSYNC_OUTPUT=$(rsync -rlpt --checksum --itemize-changes --chmod=F644 \
    "$(dirname "${BASH_SOURCE[0]}")/systemd/" \
    "$HOST:/etc/systemd/system/")
echo "$RSYNC_OUTPUT"

# --itemize-changes prefixes each file transferred TO the remote with '<f'.
CHANGED_FILES=$(echo "$RSYNC_OUTPUT" | awk '/^<f/ {print $2}')
if [[ -n $CHANGED_FILES ]]; then
  echo "==> Reloading systemd (units changed)"
  ssh "$HOST" 'systemctl daemon-reload'

  CHANGED_TIMERS=$(echo "$CHANGED_FILES" | grep '\.timer$' || true)
  if [[ -n $CHANGED_TIMERS ]]; then
    echo "==> Restarting changed timers so new schedule takes effect:"
    printf '  %s\n' $CHANGED_TIMERS
    # shellcheck disable=SC2086 # intentional word-splitting on timer list
    ssh "$HOST" "systemctl restart $CHANGED_TIMERS"
  fi
else
  echo "==> No unit file changes"
fi

cat <<EOF

Deploy complete. Next scheduled fire picks up the new binary and units.
To force an immediate run:
  ssh $HOST 'systemctl start it-digest-watch.service'
  ssh $HOST 'systemctl start it-digest-daily.service'
EOF
