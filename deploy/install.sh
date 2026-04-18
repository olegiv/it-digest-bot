#!/usr/bin/env bash
#
# it-digest-bot installer — idempotent.
#
# This script:
#   * creates the `it-digest` system user/group
#   * creates /etc/it-digest and /var/lib/it-digest with correct ownership
#   * copies the systemd unit files to /etc/systemd/system
#   * runs `systemctl daemon-reload`
#   * enables both timers
#
# It does NOT:
#   * copy the binary (you drop /usr/local/bin/digest yourself via scp)
#   * create /etc/it-digest/config.toml or /etc/it-digest/env (secrets)
#   * start any timer or service — you do that after confirming the
#     config and env files are in place
#
# Re-run as many times as you like; everything is an idempotent check.

set -euo pipefail

# ----- require root ---------------------------------------------------------

if [[ $EUID -ne 0 ]]; then
  echo "error: this script must be run as root (try: sudo $0)" >&2
  exit 1
fi

# ----- require binary -------------------------------------------------------

BINARY=/usr/local/bin/digest
if [[ ! -x $BINARY ]]; then
  cat >&2 <<EOF
error: $BINARY is missing or not executable.

Build it on your dev box with:
    make build-linux

then copy it to the server:
    scp bin/digest-linux-amd64 root@srv_prod:$BINARY
    ssh root@srv_prod chmod 755 $BINARY

then re-run this script.
EOF
  exit 1
fi

# ----- user / group ---------------------------------------------------------

if ! getent group it-digest >/dev/null; then
  groupadd --system it-digest
  echo "created group: it-digest"
else
  echo "group it-digest already exists"
fi

if ! id it-digest &>/dev/null; then
  useradd --system --gid it-digest \
          --home /var/lib/it-digest \
          --shell /usr/sbin/nologin \
          it-digest
  echo "created user:  it-digest"
else
  echo "user  it-digest already exists"
fi

# ----- directories ----------------------------------------------------------

install -d -m 0755 -o root       -g root       /etc/it-digest
install -d -m 0750 -o it-digest  -g it-digest  /var/lib/it-digest
install -d -m 0750 -o it-digest  -g it-digest  /var/backups/it-digest

# ----- backup helper --------------------------------------------------------

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
install -m 0755 -o root -g root "$SCRIPT_DIR/it-digest-backup.sh" /usr/local/bin/it-digest-backup
echo "installed /usr/local/bin/it-digest-backup"

# sqlite3 is required by the backup script; warn (don't fail) if missing.
if ! command -v sqlite3 >/dev/null 2>&1; then
  cat >&2 <<EOF
warning: sqlite3 CLI is not installed on this host. The daily backup
  timer will fail until you run:
      apt-get install sqlite3
EOF
fi

# ----- systemd units --------------------------------------------------------

UNITS_DIR=$SCRIPT_DIR/systemd

for unit in \
    it-digest-watch.service \
    it-digest-watch.timer \
    it-digest-daily.service \
    it-digest-daily.timer \
    it-digest-backup.service \
    it-digest-backup.timer \
    it-digest-notify@.service
do
  install -m 0644 -o root -g root "$UNITS_DIR/$unit" "/etc/systemd/system/$unit"
  echo "installed /etc/systemd/system/$unit"
done

systemctl daemon-reload

systemctl enable it-digest-watch.timer it-digest-daily.timer it-digest-backup.timer

# ----- next steps -----------------------------------------------------------

cat <<EOF

================================================================
Install complete. Next steps (do these yourself):

  1. Drop your config:
       sudo install -m 0640 -o root -g it-digest \\
            ./config.toml /etc/it-digest/config.toml

  2. Write the env file with the secrets:
       sudo install -m 0640 -o root -g it-digest /dev/null /etc/it-digest/env
       sudo \${EDITOR:-vi} /etc/it-digest/env
       # contents:
       #   TELEGRAM_BOT_TOKEN=1234:...
       #   ANTHROPIC_API_KEY=sk-ant-...
       #   GITHUB_TOKEN=ghp_...          # optional, raises rate limit

  3. Apply the schema migrations once:
       sudo -u it-digest /usr/local/bin/digest migrate --config /etc/it-digest/config.toml

  4. Start the timers:
       sudo systemctl start it-digest-watch.timer
       sudo systemctl start it-digest-daily.timer
       sudo systemctl start it-digest-backup.timer

  5. Watch the logs:
       journalctl -u it-digest-watch.service -f
       journalctl -u it-digest-daily.service -f
       journalctl -u it-digest-backup.service -f

================================================================
EOF
