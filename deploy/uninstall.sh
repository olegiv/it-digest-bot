#!/usr/bin/env bash
#
# Stop and remove it-digest-bot from the server. Leaves /etc/it-digest/
# (config + secrets) and /var/lib/it-digest/ (SQLite DB) in place so
# you can re-install without losing history — the operator deletes
# those manually if wanted.
#
# Usage:
#   ./deploy/uninstall.sh                  # uses DEPLOY_HOST, prompts
#   ./deploy/uninstall.sh <host>           # explicit host, prompts
#   ./deploy/uninstall.sh --yes            # DEPLOY_HOST, no prompt
#   ./deploy/uninstall.sh <host> --yes     # explicit host, no prompt

set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# uninstall.sh's argv is [host] [--yes] OR [--yes] when DEPLOY_HOST is set.
# Disambiguate: if $1 starts with --, it's the flag; otherwise it's host.
HOST_ARG=""
YES=""
for arg in "$@"; do
  case $arg in
    --yes) YES=--yes ;;
    --*) echo "unknown flag: $arg" >&2; exit 1 ;;
    *) HOST_ARG=$arg ;;
  esac
done
HOST=$(resolve_host "$HOST_ARG") || exit 1

cat <<EOF
Uninstall plan for $HOST:
  - stop + disable it-digest-watch.timer + it-digest-daily.timer
  - remove /etc/systemd/system/it-digest-*.{service,timer}
  - systemctl daemon-reload
  - delete user + group 'it-digest'
  - remove /usr/local/bin/digest

Preserved (remove by hand if you want):
  - /etc/it-digest/       (config + env secrets)
  - /var/lib/it-digest/   (SQLite state.db + posts history)
EOF

if [[ $YES != --yes ]]; then
  read -r -p "Proceed? [y/N] " reply
  [[ $reply =~ ^[Yy]$ ]] || { echo "aborted"; exit 1; }
fi

ssh "$HOST" bash <<'REMOTE'
set -euo pipefail
systemctl stop it-digest-watch.timer it-digest-daily.timer 2>/dev/null || true
systemctl disable it-digest-watch.timer it-digest-daily.timer 2>/dev/null || true
rm -f /etc/systemd/system/it-digest-watch.service \
      /etc/systemd/system/it-digest-watch.timer \
      /etc/systemd/system/it-digest-daily.service \
      /etc/systemd/system/it-digest-daily.timer
systemctl daemon-reload
if id it-digest &>/dev/null; then
  userdel it-digest
  echo "removed user:  it-digest"
fi
if getent group it-digest >/dev/null; then
  groupdel it-digest 2>/dev/null || true
  echo "removed group: it-digest"
fi
rm -f /usr/local/bin/digest
echo "binary + units + user gone. /etc/it-digest and /var/lib/it-digest preserved."
REMOTE

cat <<EOF

To fully purge (destroys config + DB):
  ssh $HOST 'rm -rf /etc/it-digest /var/lib/it-digest'
EOF
