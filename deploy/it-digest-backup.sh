#!/usr/bin/env bash
#
# Server-side daily backup: snapshot /var/lib/it-digest/state.db via
# SQLite's .backup command, gzip it, and prune files older than
# IT_DIGEST_BACKUP_KEEP_DAYS (default 14). Installed to
# /usr/local/bin/it-digest-backup and invoked by the
# it-digest-backup.service systemd unit on a daily timer.
#
# Runs as the it-digest user; both the source DB and /var/backups/it-digest/
# must be writable by that user (install.sh sets this up). Requires the
# sqlite3 CLI on the host.

set -euo pipefail

DB=/var/lib/it-digest/state.db
DIR=/var/backups/it-digest
KEEP_DAYS=${IT_DIGEST_BACKUP_KEEP_DAYS:-14}

if [[ ! -r $DB ]]; then
  echo "error: $DB is not readable" >&2
  exit 1
fi
if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "error: sqlite3 not installed — apt-get install sqlite3" >&2
  exit 1
fi

# Sweep any leftover uncompressed .db files from a prior run that was
# interrupted between sqlite3 .backup and gzip — those are dead weight.
find "$DIR" -maxdepth 1 -type f -name 'state-*.db' -delete

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$DIR/state-$STAMP.db"

# Open the source DB read-only: ProtectSystem=strict in the service unit
# mounts the filesystem read-only, and sqlite3's default is read-write —
# so the default open fails even though we never mutate the source here.
sqlite3 -readonly "$DB" ".backup '$OUT'"
gzip "$OUT"

find "$DIR" -maxdepth 1 -type f -name 'state-*.db.gz' -mtime +"$KEEP_DAYS" -delete

count=$(find "$DIR" -maxdepth 1 -type f -name 'state-*.db.gz' | wc -l | tr -d ' ')
bytes=$(du -sb "$DIR" 2>/dev/null | awk '{print $1}')
echo "saved $OUT.gz; kept $count backups, ${bytes:-0} bytes total"
