# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### `digest watch`

- Now also monitors drupal.org security advisories — Drupal core,
  contributed projects and public service announcements — from the
  combined feed `https://www.drupal.org/security/all/rss.xml`. Each
  advisory not yet in `releases_seen` (package `drupal-security`,
  version = advisory ID such as `SA-CONTRIB-2026-191`) is posted as
  one MarkdownV2 message with risk level and score, vulnerability
  type, affected versions, CVEs, a description excerpt and the
  upgrade steps, oldest first. Deterministic: no LLM call. New
  package `internal/drupalsec`; optional `[drupal_security] feed_url`
  override in `config.toml` (`https`, or `http` for `localhost`
  fixtures; credentials, query strings and fragments are rejected
  because the value is printed by `config-check` and logged on HTTP
  errors). Every header field of a post is capped, and an oversized
  advisory link falls back to `drupal.org/security`, so a post can
  never exceed Telegram's 4096-byte limit and get stuck retrying. Item
  links must be `https` on `drupal.org` (or a subdomain) with no
  credentials, whitespace or control characters; other items are
  skipped with a warning. Advisory HTML is converted to text with the
  `golang.org/x/net/html` parser, so `script`/`style` bodies are
  dropped instead of leaking into posts. Each solution line is capped
  so one long paragraph is shortened rather than dropping the whole
  Solution block, a post whose description or solution had to be
  dropped for size says so ("Shortened to fit; full details in the
  linked advisory"), and a truncated description can no longer end in
  a lone escape backslash (which Telegram rejects). Advisories that parse
  with missing fields (no ID, risk, versions or solution) are still
  posted but logged with a warning, so a drupal.org markup change is
  visible in the journal.

#### Release watcher

- New optional `releasewatch.Seeder` source extension for upstreams
  that list a window of history. When `releases_seen` has no row for
  the source's package, the runner records every current candidate
  as seen and posts the source's notice once (or nothing, if the
  source returns none) instead of announcing dozens of historical
  items on the first run. `Candidate.URL` is stored as `release_url`
  for seeded rows; seeds are audited in `posts_log` with kind `seed`;
  `--dry-run` prints a `SEED` block and writes nothing. The history is
  written in one transaction (`store.Releases.RecordSeenBatch`) after
  the notice, so a failed first run leaves no partial state that would
  turn the next run into dozens of individual posts; seeded candidates
  are validated like posted ones and must carry the seed package.
- `Runner.MaxPostsPerRun` caps send attempts per run across all sources
  (default 10, `releasewatch.DefaultMaxPostsPerRun`;
  `releasewatch.UnlimitedPosts` disables). Unseen candidates beyond the
  cap are left unrecorded and posted on later runs in source order, so
  a feed that suddenly lists many new items cannot flood the channel in
  one pass, and a Telegram outage stops after the cap instead of
  retrying every release. Seeding is not subject to the cap.

#### Telegram

- `telegram.EscapeMarkdownV2Plain` escapes every MarkdownV2 special
  character without preserving links or code spans. `internal/drupalsec`
  uses it for all feed-derived text, so markup inside an upstream
  advisory (a `[text](url)` link, a backtick span) renders as literal
  text instead of live markup in the channel.

### Changed

#### Release watcher

- `releasewatch.ItemResult` reports a single `Outcome` (`OutcomeError`,
  `OutcomePosted`, `OutcomeSeen`, `OutcomeDeferred`, `OutcomeSeeded`,
  `OutcomeCapped`, `OutcomeRendered` for dry runs) instead of five booleans, so the states are mutually
  exclusive by construction and a candidate that failed before reaching
  one is reported as an error rather than as a success with no flag.
  `Result.Count(Outcome)` replaces the three hand-written loops behind
  `PostedCount`, `SeededCount` and `CappedCount`, which remain.
- `drupalsec.Advisory.Kind` is now a method derived from `ID` rather
  than a stored field, so the two can never disagree.

### Fixed

#### Daily digest LLM JSON output

- `digest daily` no longer fails with
  `summarize: no JSON array found in model output` when the
  model adds a chain-of-thought preamble. The Anthropic
  request now uses tool-use with a forced `tool_choice` of
  `submit_summaries`, whose JSON Schema declares the exact
  shape of each entry; the response comes back as
  structured `tool_use.input` rather than free-form text,
  which both eliminates fragile text parsing and prevents
  reasoning preambles from exhausting `max_tokens` before
  any usable output. Removed the now-unused
  `extractJSONArray` / `parseSummaries` helpers and their
  tests. Parse-stage failures still surface the API
  `stop_reason`, and a truncated response (no `tool_use`
  block, `stop_reason="max_tokens"`) produces a distinct
  error pointing the operator at `llm.max_tokens` or the
  candidate count.

#### Daily SQLite backup

- `it-digest-backup.service` no longer fails with
  `unable to open database file`. SQLite's WAL mode mmaps a
  `-shm` sidecar in the database directory even on read-only
  opens, and the unit's `ReadOnlyPaths=/var/lib/it-digest`
  blocked that. Switched to `ReadWritePaths`; the script still
  uses `sqlite3 -readonly` so the source DB is never mutated,
  and the unit still runs as the dedicated `it-digest` user
  under `ProtectSystem=strict`.

## [0.2.0] — 2026-04-27

### Added

#### `digest watch`

- Now also monitors official stable Go releases from
  `go.dev/dl/?mode=json`, posts one announcement per unseen Go
  version, and includes release-history text when available.

### Changed

#### Release watcher

- Release posting now uses a shared `internal/releasewatch` runner
  for common seen-check, dry-run, Telegram send, and audit-log
  behavior across sources.

#### Daily digest

- `digest daily` now caps the number of items per source so a
  big-news day on one provider does not bury lower-traffic feeds.
  Default cap is 2; configurable via `digest.max_per_source`
  (negative value disables).

#### Toolchain

- Go updated to `1.26.2` and `modernc.org/sqlite` to `v1.50.0`.
  `govulncheck` reports clean. No schema or behavior change for
  operators.

### Fixed

#### `digest watch`

- No longer re-posts a release that is already in `releases_seen`
  after npm's `dist-tags.latest` rolls back. The duplicate-detection
  check now looks up `(package, version)` directly via
  `Releases.HasSeen` instead of comparing against the
  most-recently-posted row, which could be a different (newer)
  version than the one npm currently considers latest.
- Now requires npm `dist-tags.latest` and GitHub `/releases/latest`
  to name the same version before posting. A short-lived npm publish
  that gets demoted (or never gets a corresponding GitHub Release)
  no longer triggers a Telegram announcement; the bot defers
  silently until both signals agree. Draft and prerelease responses
  from GitHub are rejected as defense-in-depth, even though the
  endpoint already filters them.
- A GitHub `/releases/latest` 404 against an inaccessible repository
  (private, deleted, renamed, or `GITHUB_TOKEN` revoked) is now
  surfaced as a hard error so the systemd `OnFailure=` notify alert
  fires. Previously a typo'd `github_repo` would silently disable
  release announcements forever. The legitimate "no qualifying
  release yet" 404 still defers cleanly.

#### HTTP retry

- POST/PUT requests with seekable bodies now refresh their body via
  `Request.GetBody` on retry. Previously a transport-level retry
  could replay an exhausted body and fail with
  `http: ContentLength=N with Body length 0` — and that error path
  leaked the raw URL, including Telegram bot tokens, past the
  `telegram.SanitizeURL` mask.

### Security

#### HTTP client

- Closes LOW-001 from the 2026-04-26 audit. Six trusted-API clients
  (npm, GitHub releases, Anthropic `/v1/messages`, Telegram
  `sendMessage`) now read upstream JSON through `io.LimitReader`
  with explicit post-read size checks: 4 MiB for
  npm/GitHub/Anthropic, 1 MiB for Telegram. The 30 s `httpx`
  timeout already bounds time; this bounds bytes against a
  misbehaving upstream.

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

[Unreleased]: https://github.com/olegiv/it-digest-bot/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/olegiv/it-digest-bot/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/olegiv/it-digest-bot/releases/tag/v0.1.0
