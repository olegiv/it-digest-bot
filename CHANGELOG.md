# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-04-19

Initial public release.

### Added

#### Subcommands

- `digest watch` (hourly systemd timer) — polls npm `dist-tags.latest` for
  `@anthropic-ai/claude-code`, fetches release notes from GitHub (falls back to
  `CHANGELOG.md` in the upstream repo), and posts a MarkdownV2 announcement to
  a configured Telegram channel.
- `digest daily` (08:00 Europe/Zurich systemd timer) — fetches all configured
  RSS/Atom feeds in parallel, dedupes against `articles_seen`, sends the 24–48h
  window to Anthropic's Claude API for ranking and summarization, posts a
  grouped MarkdownV2 digest to Telegram (auto-splits messages > 4096 B).
- `digest migrate` — applies embedded SQLite schema migrations from
  `migrations/`.
- `digest config-check` — loads and fully validates the config + env; exits
  nonzero on the first problem with a descriptive message.
- `digest notify --unit <name>` (hidden; systemd `OnFailure=` hook) — posts an
  alert with host, timestamp, and unit name to the admin Telegram chat (falls
  back to the main channel if `[telegram] admin_chat` is unset).
- `digest post --dry-run` — renders a sample release post to stdout.
- `digest version` — prints version, commit sha, and build date.

#### Infrastructure

- Single static Go binary (no CGo), scheduled entirely by systemd timers. No
  Docker, no web server, no long-running daemon.
- Server-side daily SQLite backup: `sqlite3 -readonly .backup` → gzip →
  `/var/backups/it-digest/state-<UTC>.db.gz`, 14-day retention, fires at 03:00
  Europe/Zurich.
- systemd units fully sandboxed (`NoNewPrivileges`, `ProtectSystem=strict`,
  `RestrictAddressFamilies`, `SystemCallFilter=@system-service`,
  `ProtectKernel*`, `RestrictNamespaces`, etc.).
- Prod binary built as PIE (`-buildmode=pie`) targeting `x86-64-v3`.

#### Ops tooling

- `make deploy` / `make rollback` — test-gated push of
  `bin/digest-linux-amd64` to `/usr/local/bin/digest`; rollback swaps to a
  preserved `.prev` on failure.
- `make backup` / `make status` / `make dry-watch` / `make dry-daily` /
  `make uninstall` — client-side helpers backed by `deploy/*.sh`.
- `DEPLOY_HOST` auto-loaded from `deploy/deploy.env` (gitignored).

### Security

- Telegram bot tokens embedded in URL paths (`/bot<TOKEN>/sendMessage`) are
  defensively masked in retry-exhausted `httpx.Client` errors via
  `telegram.SanitizeURL`; feed URLs with API keys in query strings are masked
  via `news.SanitizeURL`. Both sanitizers are installed unconditionally by
  `telegram.New` and `news.NewFeedFetcher` so a caller-supplied `httpx.Client`
  can't silently drop the protection.
- All SQL parameterized (`internal/store/*.go`). No string concatenation into
  query text.
- Feed HTML parsed via `golang.org/x/net/html` (not a hand-rolled stripper);
  `<script>` and `<style>` subtrees dropped so adversarial markup can't smuggle
  text into the LLM prompt.
- Secrets env-only (`TELEGRAM_BOT_TOKEN`, `ANTHROPIC_API_KEY`, optional
  `GITHUB_TOKEN`); `toml:"-"` tags on every sensitive field.
- `govulncheck ./...` clean; all direct dependencies at latest stable.
- Full local security audit, all actionable findings (1 HIGH, 4 MEDIUM, 4 LOW)
  closed in source before the initial release.

[Unreleased]: https://github.com/olegiv/it-digest-bot/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/olegiv/it-digest-bot/releases/tag/v0.1.0
