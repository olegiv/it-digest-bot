# it-digest-bot

[![CI](https://github.com/olegiv/it-digest-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/olegiv/it-digest-bot/actions/workflows/ci.yml) [![CodeQL](https://github.com/olegiv/it-digest-bot/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/olegiv/it-digest-bot/actions/workflows/github-code-scanning/codeql)

Posts Claude Code and Go release announcements — plus daily AI news digests — to a Telegram channel. Single static Go binary, scheduled by **systemd timers** on plain Ubuntu. No Docker, no web server, no long-running daemon.

## Architecture

```
  systemd timer (hourly)
          │
          ▼
 ┌─────────────────┐      HTTPS      ┌──────────────┐
 │  digest watch   │ ─────────────▶  │    npm       │   dist-tags.latest
 │ (one-shot run)  │ ◀─────────────  └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │    GitHub    │   latest release
 │                 │ ◀─────────────  │    API       │   notes
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │    go.dev    │   stable Go releases
 │                 │ ◀─────────────  │  dl + docs   │   + release history
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │   Telegram   │   sendMessage
 │                 │ ◀─────────────  │     Bot      │
 │                 │                 └──────────────┘
 │                 │                 ┌──────────────┐
 │                 │ ─ reads/writes─▶│    SQLite    │   releases_seen
 └─────────────────┘                 │  (file only) │   articles_seen
                                     │              │   posts_log
                                     └──────────────┘

  systemd timer (08:00 Europe/Zurich)
          │
          ▼
 ┌─────────────────┐      HTTPS      ┌──────────────┐
 │  digest daily   │ ─────────────▶  │   6 feeds    │   parallel fetch (errgroup)
 │ (one-shot run)  │ ◀─────────────  │  (RSS/Atom)  │
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │  Anthropic   │   /v1/messages
 │                 │ ◀─────────────  │  (Claude)    │   rank + summarize
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │   Telegram   │   sendMessage × N
 │                 │ ◀─────────────  │     Bot      │   (split if > 4096B)
 └─────────────────┘                 └──────────────┘
```

Each run exits on completion. journald stores the logs. Re-runs are idempotent because everything that mutates state is keyed off `releases_seen (package, version)` or `articles_seen.url_hash`.

## Quick start (local dev)

```bash
# 1. build
make build

# 2. copy and edit config
cp config.example.toml config.toml

# 3. set secrets in your shell
export TELEGRAM_BOT_TOKEN=123456:AA...
# ANTHROPIC_API_KEY=sk-ant-...   (phase 2 only)

# 4. render a fake release post to stdout (no network)
./bin/digest post --dry-run --config config.toml

# 5. apply SQLite migrations to a local db
./bin/digest migrate --config config.toml

# 6. do a real check against npm + GitHub + go.dev + Telegram
./bin/digest watch --config config.toml
```

## Production install (Ubuntu 24.04 + systemd)

```bash
# On your dev machine
make build-linux-amd64
scp bin/digest-linux-amd64 root@srv_prod:/usr/local/bin/digest
ssh root@srv_prod chmod 755 /usr/local/bin/digest

# On the server (as root)
cd /path/to/deploy
./install.sh         # creates user, dirs, installs units, reloads systemd

# Drop your TOML config
sudo install -m 0640 -o root -g it-digest ./config.toml /etc/it-digest/config.toml

# Write the env file with the secrets
sudo install -m 0640 -o root -g it-digest /dev/null /etc/it-digest/env
sudoedit /etc/it-digest/env
#   TELEGRAM_BOT_TOKEN=...
#   ANTHROPIC_API_KEY=...
#   GITHUB_TOKEN=...      # optional, raises GitHub rate limit 60→5000 req/h

# Apply schema
sudo -u it-digest /usr/local/bin/digest migrate --config /etc/it-digest/config.toml

# Start the timers
sudo systemctl start it-digest-watch.timer
sudo systemctl start it-digest-daily.timer

# Watch
journalctl -u it-digest-watch.service -f
journalctl -u it-digest-daily.service -f
```

### Operations

Once the server is bootstrapped, five ops helpers live in `deploy/` and are also exposed as Makefile targets. Every helper resolves the target host from (in order): an explicit argument, the `HOST` env var, or `DEPLOY_HOST` — which is auto-loaded from `deploy/deploy.env` (gitignored). Copy `deploy/deploy.env.example` to `deploy/deploy.env` and set `DEPLOY_HOST` once; subsequent `make deploy`, `make status`, etc. work with no args. Bare hostnames default to `root@<host>`; prefix explicitly for a different user.

| Makefile | Script | What it does |
|---|---|---|
| `make deploy` | `deploy/deploy.sh` | Run race tests (skip with `SKIP_TESTS=1`), build + scp `bin/digest-linux-amd64` to `/usr/local/bin/digest` (preserving the previous binary as `.prev`). Then rsync `deploy/systemd/` to `/etc/systemd/system/` — if any unit changed, `daemon-reload`; if any timer changed, restart it so the new schedule takes effect. Next timer fire uses the new binary and units. |
| `make rollback` | `deploy/rollback.sh` | Swap `digest` ↔ `digest.prev` to revert a bad deploy. The failed binary is kept as `digest.failed` for inspection. |
| `make backup` | `deploy/backup.sh` | `sqlite3 .backup` snapshot of `/var/lib/it-digest/state.db` → local `backups/state-<UTC-stamp>.db`. Requires `sqlite3` on the server. |
| `make status` | `deploy/status.sh` | `systemctl list-timers` + binary version + last 20 journal lines for both services. |
| `make dry-watch` | `deploy/dry-run.sh watch` | Run `digest watch --dry-run` on the server with the real env file loaded (no post, no DB write). |
| `make dry-daily` | `deploy/dry-run.sh daily` | Run `digest daily --dry-run` on the server with the real env file loaded (no post, no DB write). |
| `make uninstall` | `deploy/uninstall.sh` | Stop + disable timers, remove units, delete user + binary. Preserves `/etc/it-digest/` and `/var/lib/it-digest/`. Prompts unless given `--yes`. |

A third systemd timer handles **daily server-side backups** automatically — no client interaction needed:

| Component | Notes |
|---|---|
| `/usr/local/bin/it-digest-backup` | Script installed by `install.sh`. Runs `sqlite3 .backup` → gzip → `/var/backups/it-digest/state-<UTC>.db.gz`, prunes files older than `IT_DIGEST_BACKUP_KEEP_DAYS` (default 14). Requires `sqlite3` on the host. |
| `it-digest-backup.service` | Oneshot, runs as `it-digest`, fully sandboxed (no network, read-only FS except `/var/backups/it-digest`). |
| `it-digest-backup.timer` | `OnCalendar=*-*-* 03:00:00 Europe/Zurich`, `Persistent=true` — fires once a day, safely before the 08:00 daily digest. |

`make backup HOST=x` (client-side) still works for ad-hoc local snapshots; the server-side timer is the automatic background safety net.

## Subcommands

| Command                 | What it does                                                       |
|-------------------------|--------------------------------------------------------------------|
| `digest watch`          | Check Claude Code and stable Go releases; post new versions.        |
| `digest daily`          | Build and post the daily AI news digest via Claude.                |
| `digest post --dry-run` | Render a sample release post to stdout without sending anything.   |
| `digest migrate`        | Apply pending SQLite schema migrations.                            |
| `digest config-check`   | Load and fully validate the config + env; exit nonzero on error.   |
| `digest -version`       | Print version, commit, and build date.                             |

All subcommands accept `--config <path>` (default: `config.toml`).

## Configuration reference

All non-secret settings live in `config.toml`. See [`config.example.toml`](./config.example.toml) for a commented starter.

| Section           | Key            | Required | Default                  | Notes |
|-------------------|----------------|----------|--------------------------|-------|
| `[telegram]`      | `channel`      | yes      | —                        | e.g. `@your_channel` |
| `[telegram]`      | `admin_chat`   | no       | (falls back to `channel`) | Optional destination for OnFailure alerts |
| `[database]`      | `path`         | yes      | —                        | SQLite file path |
| `[claudecode]`    | `npm_package`  | yes      | `@anthropic-ai/claude-code` | |
| `[claudecode]`    | `github_repo`  | yes      | `anthropics/claude-code` | |
| `[llm]`           | `model`        | phase 2  | `claude-sonnet-4-6`      | |
| `[llm]`           | `max_tokens`   | phase 2  | `1024`                   | |
| `[log]`           | `level`        | no       | `info`                   | `debug` / `info` / `warn` / `error` |
| `[log]`           | `format`       | no       | `json`                   | `json` / `text` |
| `[[feed]]`        | `name`, `url`  | phase 2  | —                        | One block per feed |

Go release monitoring has no TOML settings; `digest watch` reads official stable releases from `https://go.dev/dl/?mode=json`.

Secrets come from the environment only (never the TOML):

| Env var                | Required? | Purpose |
|------------------------|-----------|---------|
| `TELEGRAM_BOT_TOKEN`   | yes       | Any command that posts to Telegram |
| `ANTHROPIC_API_KEY`    | yes (daily) | `digest daily` (phase 2) |
| `GITHUB_TOKEN`         | optional  | Raises GitHub rate limit from 60→5000 req/hour; `public_repo` scope is enough |

## Roadmap

- [x] **Phase 1** — Release watchers (Claude Code via npm/GitHub, Go via go.dev → Telegram).
- [x] **Phase 2** — Daily AI digest (RSS aggregation → Claude summarization → Telegram).

Both phases are fully implemented. The `daily` command fetches all configured feeds in parallel, dedupes against `articles_seen`, sends the 24h window to Claude via `POST /v1/messages` for ranking/summarization, renders a MarkdownV2 post grouped by source, and splits into chunks under Telegram's 4096-byte cap if needed.

See [CHANGELOG.md](./CHANGELOG.md) for release notes.

## Development

```bash
make help            # list Makefile targets
make all             # build the default local/dev binary
make build           # fast local/dev build
make build-prod      # optimized host production build
make build-linux-amd64  # optimized static linux/amd64 production build
make build-darwin-arm64 # optimized darwin/arm64 production build
make build-all-platforms # linux/amd64 + darwin/arm64 production builds
make install-tools   # golangci-lint + gofumpt at pinned versions
make test            # go test ./...
make test-race       # with -race
make coverage        # go test -cover ./...
make coverage-html   # write coverage.out + coverage.html
make lint            # golangci-lint
make check           # fmt-check + vet + lint + test
make deps            # go mod download
make tidy            # go mod tidy
make fmt             # gofumpt -w .
```

CI (GitHub Actions) runs build + vet + race tests + lint on every push and PR against `main`.

Test coverage targets (spec): ≥ 70% on `internal/claudecode`, `internal/telegram`, `internal/config`. Current (all above target):

| Package           | Coverage |
|-------------------|----------|
| claudecode        | ~82%     |
| telegram          | ~79%     |
| config            | ~77%     |
| digest            | ~82%     |
| llm               | ~79%     |
| news              | ~91%     |

## Contributing

PRs welcome. Before opening one:

1. `make check` and `make test-race` must pass.
2. Keep commits logically scoped (one concern per commit).
3. No `fmt.Println` or `log.Printf` — use `log/slog`.
4. Errors wrapped with `fmt.Errorf("...: %w", err)` — never bare.
5. Context threaded through any function that does I/O.

## License

GPL-3.0. See [LICENSE](./LICENSE).
