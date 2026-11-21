# it-digest-bot

[![Telegram channel](https://img.shields.io/badge/channel-@it__digest__info-26A5E4?logo=telegram)](https://t.me/it_digest_info)
[![CI](https://github.com/olegiv/it-digest-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/olegiv/it-digest-bot/actions/workflows/ci.yml)
[![Vulncheck](https://github.com/olegiv/it-digest-bot/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/olegiv/it-digest-bot/actions/workflows/vulncheck.yml)
[![CodeQL](https://github.com/olegiv/it-digest-bot/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/olegiv/it-digest-bot/actions/workflows/github-code-scanning/codeql)
[![Latest release](https://img.shields.io/github/v/release/olegiv/it-digest-bot)](https://github.com/olegiv/it-digest-bot/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/olegiv/it-digest-bot?logo=go)](go.mod)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

Posts Claude Code, Go, PHP and stable iOS/macOS release announcements and new Drupal and PHP security advisories (plus opt-in Ubuntu and Debian security advisories for chosen packages) — plus daily AI news digests — to the Telegram channel [@it_digest_info](https://t.me/it_digest_info). Single static Go binary, scheduled by **systemd timers** on plain Ubuntu. No Docker, no web server, no long-running daemon.

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
 │                 │ ─────────────▶  │   php.net    │   PHP branch releases
 │                 │ ◀─────────────  │ active.php   │   (security-tagged)
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │  GitHub API  │   PHP security
 │                 │ ◀─────────────  │  php-src     │   advisories (GHSA)
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │  drupal.org  │   security advisories
 │                 │ ◀─────────────  │  RSS feed    │   core + contrib + PSA
 │                 │                 └──────────────┘
 │                 │      HTTPS      ┌──────────────┐
 │                 │ ─────────────▶  │ developer.   │   stable iOS / macOS
 │                 │ ◀─────────────  │ apple.com RSS│   releases
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

Requires Go 1.27.4 or newer (the version in `go.mod`; earlier 1.27.x releases have known vulnerabilities in the standard library).

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

# 6. do a real check against npm + GitHub + go.dev + php.net + drupal.org + developer.apple.com + Telegram
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
| `digest watch`          | Check Claude Code, stable Go, PHP and stable iOS/macOS releases and new Drupal, PHP and (opt-in) Ubuntu/Debian security advisories; post what is new. |
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
| `[telegram]`      | `admin_chat`   | no       | (falls back to `channel`) | Destination for OnFailure alerts. Set it to a private chat: alerts that fall back to the public channel omit the hostname, and `config-check` warns |
| `[database]`      | `path`         | yes      | —                        | SQLite file path |
| `[claudecode]`    | `npm_package`  | yes      | `@anthropic-ai/claude-code` | |
| `[claudecode]`    | `github_repo`  | yes      | `anthropics/claude-code` | |
| `[drupal_security]` | `feed_url`   | no       | `https://www.drupal.org/security/all/rss.xml` | Override only; `https`, or `http` for `localhost`/loopback fixtures; no credentials, query string or fragment |
| `[apple_releases]` | `feed_url`    | no       | `https://developer.apple.com/news/releases/rss/releases.rss` | Override only; same rules as `drupal_security.feed_url` |
| `[php]`           | `releases_url` | no       | `https://www.php.net/releases/active.php` | Override only; same rules as `drupal_security.feed_url` |
| `[php]`           | `advisories_url` | no     | `https://api.github.com/repos/php/php-src/security-advisories` | Override only; same rules as `drupal_security.feed_url`; `GITHUB_TOKEN` is sent only to `https://api.github.com` |
| `[ubuntu_security]` | `packages`   | no       | — (disabled)             | Source packages to watch (`nginx`, `openssl`, `php8.3`, …); the Ubuntu source runs only when set |
| `[ubuntu_security]` | `release`    | no       | `noble`                  | Ubuntu release codename (`noble` = 24.04 LTS) |
| `[ubuntu_security]` | `notices_url` | no      | `https://ubuntu.com/security/notices.json` | Override only; same rules as `drupal_security.feed_url` |
| `[debian_security]` | `packages`   | no       | — (disabled)             | Source packages to watch (`php8.4`, `postgresql-17`, …); the Debian source runs only when set |
| `[debian_security]` | `feed_url`   | no       | `https://www.debian.org/security/dsa-long.rdf` | Override only; same rules as `drupal_security.feed_url` |
| `[llm]`           | `model`        | phase 2  | —                       | Recommended: `claude-sonnet-5-5`; must be set explicitly |
| `[llm]`           | `max_tokens`   | phase 2  | —                       | Recommended: `2048`; must be set explicitly |
| `[llm]`           | `timeout_seconds` | no    | `120`                   | Per-request Anthropic timeout |
| `[log]`           | `level`        | no       | `info`                   | `debug` / `info` / `warn` / `error` |
| `[log]`           | `format`       | no       | `json`                   | `json` / `text` |
| `[[feed]]`        | `name`, `url`  | phase 2  | —                        | One block per feed |

Go release monitoring has no TOML settings; `digest watch` reads official stable releases from `https://go.dev/dl/?mode=json`.

For `claude-sonnet-5-5`, the daily digest requests schema-constrained JSON
with `thinking.type = "between_tools"` and medium effort, avoiding up-front
thinking. Set `max_tokens = 2048` to leave room for complete summaries.
Sonnet 4.6 and previously configured models retain the forced-tool request
path, so switching back to the previous model remains supported. Model,
request duration, and API token usage are logged without candidate content.

Drupal security monitoring is always on as well. `digest watch` reads the combined drupal.org security feed (core, contributed projects and public service announcements) and posts one message per advisory that is not yet in `releases_seen`, oldest first, with risk level, vulnerability type, affected versions, CVEs and the upgrade path. On its very first run against a database the feed's current items (50 at the time of writing) are recorded as the baseline and a single "now tracked" notice is posted instead of one announcement each.

Drupal feed HTTP 404 responses get up to three attempts, with waits of 10 and
30 seconds, adding up to 40 seconds of retry delay plus request time. Each
failed response is closed before waiting, and cancellation interrupts the wait.
Retry warnings include the source, status, attempt number and delay. If all
three attempts return 404, no Drupal advisories are recorded; healthy sources
continue, and the watcher exits nonzero, triggering the existing systemd failure
alert. Network errors, HTTP 429 and HTTP 5xx retain the shared HTTP retry policy;
the additional 404 retries apply only to the Drupal security feed.

The shared HTTP retry policy honours an upstream `Retry-After` of up to 60
seconds. A longer one ends the retries for that request at once, so a slow or
hostile upstream cannot stall the run. As a backstop, `it-digest-watch.service`
sets `TimeoutStartSec=10min`: a run that still hangs is stopped and marked
failed, which sends the usual failure alert.

Stable iOS and macOS releases are always tracked too. `digest watch` reads the Apple Developer releases feed and keeps only titles of the exact form `iOS 27.0.1 (24A446)` / `macOS Tahoe 26.1 (25B78)`; betas, RCs, iPadOS/watchOS/tvOS/visionOS and Xcode are ignored. Each new version (deduplicated per platform under `releases_seen` package `ios` / `macos`) is posted with its build number, release-notes link and Apple's security-content index. Updates to an older major branch (e.g. iOS 18.7.10 while iOS 27 is current) are posted too, marked as an older-branch update. As with Drupal, each platform's first run records the feed's current releases as the baseline and posts a single "now tracked" notice.

PHP is tracked by two always-on sources that run in the same hourly `digest watch` pass as Go:

- **Releases** (`releases_seen` package `php`): php.net's `releases/active.php` lists the latest release of every supported branch. Each new branch tip is posted with its branch status (first release of a new branch, current branch, or older branch), the release date, and links to the announcement, ChangeLog and downloads. Releases php.net tags as `security` are marked as security releases. A coordinated multi-branch release is posted oldest branch first.
- **Security advisories** (`releases_seen` package `php-security`, keyed by GHSA ID): the PHP project publishes its advisories as GitHub security advisories on `php/php-src`. Each new published, non-withdrawn advisory is posted with severity and CVSS score, CVE, CWE, affected and fixed versions, and the first paragraph of the description; `scheme://…` and `www.…` links in the summary and excerpt are removed except https links to php.net and github.com/php/…, since Telegram would auto-link them; bare domains (`example.com`) are not removed. One unauthenticated request per hour fits GitHub's 60 requests/hour limit; `GITHUB_TOKEN` raises it. An empty advisory list from GitHub fails the run (php-src always has advisories), so the first-run baseline is only taken from a real list.

Both PHP sources seed on their first run, like Drupal: the current branch tips and the published advisories are recorded as the baseline and one "now tracked" notice is posted per source.

Ubuntu and Debian security advisories are opt-in and meant for the software your web servers run. Each source runs only when its `packages` list is set in `[ubuntu_security]` / `[debian_security]`:

- **Ubuntu** (`releases_seen` package `usn-<release>`): Ubuntu Security Notices for the configured release (`noble` = 24.04 LTS) from `https://ubuntu.com/security/notices.json`, newest 40 per run.
- **Debian** (package `dsa`): Debian Security Advisories from `https://www.debian.org/security/dsa-long.rdf`. Versioned names (`php8.4`, `postgresql-17`) select a release; unversioned ones (`nginx`, `openssl`, `linux`) match DSAs for every supported Debian release, since DSAs are not filtered by release.

Every advisory that touches a watched source package is posted, local issues included. Neither feed carries CVSS vectors or priorities, so the post's labels are read from the security teams' consistent wording ("A remote attacker could …"); kernel subsystem lists ("Remote Processor subsystem") are not remote cues. Posts mark remotely exploitable issues and show the likely impact (code execution, privilege escalation, information disclosure, denial of service, …), whether the fix needs Ubuntu Pro, the update command and, for Ubuntu only, CVEs and the fixed version. `scheme://…` and `www.…` links in the advisory text are removed except https links to ubuntu.com and debian.org, since Telegram would auto-link them; bare domains are not removed. An Ubuntu fetch that returns no notice for the configured release fails the run, so an API change or a mistyped codename triggers the failure alert instead of silence. Advisories older than 7 days are not posted, so enabling a source or adding a package posts at most last week's advisories; there is no first-run baseline, because a filtered list is often empty and a baseline would swallow the first real alert. An older advisory that was never posted (sends kept failing, or the bot was down for more than a week) is recorded as seen with one `release never posted: expired` warning, so it leaves a trace instead of vanishing; enabling a source logs this once for each older advisory still in the feed. The lists take source package names (`nginx`, `php8.3`, `openssl`); a binary name such as `php8.3-fpm` or `libssl3` that appears in an Ubuntu notice is reported with a warning naming its source package. Diagnostics are warnings in the journal: notices without a source package, and DSA titles that do not have the usual `DSA-n-n package - kind` shape (such advisories are still posted). Posts go to the public channel; this is an accepted choice. Watch the latest Debian and Ubuntu releases with a broad list of the most commonly used server packages (the lists in `config.example.toml`) rather than a list that mirrors one server:

- Every post is a public Ubuntu or Debian advisory; nothing in it is secret.
- A broad list (nginx *and* apache2, mariadb *and* postgresql, both distros) does not tell an observer which of them a server runs.
- Posts appear when an advisory is published, not when a server is patched, so they say nothing about whether a server is still vulnerable.
- A web server's software is visible anyway (response headers, TLS, error pages), and attackers scan for exploits regardless.

Keep niche packages that only one server uses off the lists: each post for them hints that you run them. The protection itself comes from patching quickly, so keep `unattended-upgrades` enabled on the servers and reboot after kernel updates.

Secrets come from the environment only (never the TOML):

| Env var                | Required? | Purpose |
|------------------------|-----------|---------|
| `TELEGRAM_BOT_TOKEN`   | yes       | Any command that posts to Telegram |
| `ANTHROPIC_API_KEY`    | yes (daily) | `digest daily` (phase 2) |
| `GITHUB_TOKEN`         | optional  | Raises GitHub rate limit from 60→5000 req/hour. Only public data is read: a fine-grained token with read-only public repository access and no permissions is enough. Sent only to `api.github.com` |

## Roadmap

- [x] **Phase 1** — Release watchers (Claude Code via npm/GitHub, Go via go.dev, PHP via php.net, PHP security advisories via the GitHub API, Drupal security advisories via drupal.org RSS, iOS/macOS via the Apple Developer releases RSS → Telegram).
- [x] **Phase 2** — Daily AI digest (RSS aggregation → Claude summarization → Telegram).

Both phases are fully implemented. The `daily` command fetches all configured feeds in parallel, dedupes against `articles_seen`, sends the 24h window to Claude via `POST /v1/messages` for ranking/summarization, renders a MarkdownV2 post grouped by source, and splits into chunks under Telegram's 4096-byte cap if needed.

See [CHANGELOG.md](./CHANGELOG.md) for release notes.

## Development

Shared Claude Code agents, commands and hooks come from the
[`olegiv/claude-code-support-tools`](https://github.com/olegiv/claude-code-support-tools)
submodule at `.claude/shared`; `.claude/agents`, `.claude/commands` and
`.claude/hooks` symlink into it. Initialise it once per clone, or the
symlinks dangle and the Go hooks in `.claude/settings.json` do not run:

```bash
git submodule update --init          # or clone with --recurse-submodules
```

```bash
make help            # list Makefile targets
make all             # build the default local/dev binary
make build           # fast local/dev build
make build-prod      # optimized host production build
make build-linux-amd64  # optimized static linux/amd64 production build
make build-darwin-arm64 # optimized darwin/arm64 production build
make build-all-platforms # linux/amd64 + darwin/arm64 production builds
make install-tools   # golangci-lint + gofumpt + govulncheck at pinned versions
make test            # go test ./...
make test-race       # with -race
make coverage        # go test -cover ./...
make coverage-html   # write coverage.out + coverage.html
make lint            # golangci-lint
make vulncheck       # govulncheck (known vulnerabilities; needs network)
make check           # fmt-check + vet + lint + test
make deps            # go mod download
make tidy            # go mod tidy
make fmt             # gofumpt -w .
```

CI (GitHub Actions) runs build + vet + race tests + lint on every push and PR against `main`. Dependabot opens one grouped PR per week for GitHub Actions and one for Go modules (`.github/dependabot.yml`); it leaves `modernc.org/libc` and `modernc.org/memory` alone because they must match `modernc.org/sqlite`. A separate workflow runs `govulncheck` on every push and PR against `main` and weekly, since new advisories appear without code changes.

Test coverage targets (spec): ≥ 70% on `internal/claudecode`, `internal/telegram`, `internal/config`. Current (all above target):

| Package           | Coverage |
|-------------------|----------|
| applerelease      | ~92%     |
| claudecode        | ~83%     |
| config            | ~89%     |
| digest            | ~87%     |
| distrosec         | ~97%     |
| drupalsec         | ~98%     |
| gorelease         | ~83%     |
| httpx             | ~82%     |
| llm               | ~88%     |
| news              | ~91%     |
| phprelease        | ~98%     |
| phpsec            | ~96%     |
| pkgname           | ~100%    |
| releasewatch      | ~89%     |
| store             | ~78%     |
| strs              | ~100%    |
| telegram          | ~92%     |
| version           | ~100%    |

## Contributing

PRs welcome. Before opening one:

1. `make check` and `make test-race` must pass; `make vulncheck` should report no vulnerabilities.
2. Keep commits logically scoped (one concern per commit).
3. No `fmt.Println` or `log.Printf` — use `log/slog`.
4. Errors wrapped with `fmt.Errorf("...: %w", err)` — never bare.
5. Context threaded through any function that does I/O.

## License

GPL-3.0. See [LICENSE](./LICENSE).
