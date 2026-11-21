# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Dependencies: `golang.org/x/net` v0.63.0, `golang.org/x/sync` v0.25.0,
  `modernc.org/sqlite` v1.62.0 (with `modernc.org/libc` v1.80.1 and
  `modernc.org/memory` v1.12.1, the exact versions sqlite v1.62.0
  requires), `golang.org/x/sys` v0.50.0, `golang.org/x/text` v0.44.0 and
  `github.com/spf13/pflag` v1.1.0. Routine updates; govulncheck reported
  nothing for the previous versions.
- `modernc.org/libc` and `modernc.org/memory` were briefly raised past the
  versions `modernc.org/sqlite` is built against (libc v1.82.2 and memory
  v1.13.0 instead of v1.80.1 and v1.12.1) by a blanket `go get -u`;
  modernc.org/sqlite requires the exact libc version from its own go.mod.
  They are pinned back, and `TestModerncPinnedToSQLite` now fails whenever
  they drift.
- PHP security advisory posts drop the URLs and `www.` tokens in the
  advisory summary and excerpt (new `telegram.StripBareLinks`), except
  links to php.net and to github.com/php/…: the text is posted as plain
  text, where Telegram clients would auto-link any URL the advisory author
  wrote.
- The PHP security advisory source fails the run when GitHub returns an
  empty advisory list instead of returning no candidates. php-src always
  has published advisories, so an empty list is an upstream glitch;
  before, the next non-empty run would have taken a brand-new advisory
  into the first-run baseline instead of posting it.
- `telegram.CleanText` also removes invisible characters that are not
  format characters: variation selectors, private-use characters,
  noncharacters, Hangul fillers (U+115F, U+1160, U+3164, U+FFA0), U+034F
  and U+17B4/U+17B5; U+2800 BRAILLE PATTERN BLANK becomes a space. They
  could fake empty or padded fields in posts.
- All four systemd units add `CapabilityBoundingSet=` (no capabilities),
  `AmbientCapabilities=`, `PrivateDevices`, `ProtectClock`,
  `ProtectHostname`, `ProtectKernelLogs`, `ProtectProc=invisible`,
  `RestrictRealtime`, `RemoveIPC` and `UMask=0077` (state DB, WAL files
  and backups private to the service user). `systemd-analyze security`
  exposure drops from 4.4-4.5 to 1.2 per unit. Re-run
  `deploy/install.sh` to install them.
- Source-package and Ubuntu-release validation live in the new leaf
  package `internal/pkgname`, shared by `internal/config` and
  `internal/distrosec` (DSA titles included), so config and feeds apply
  the same rules.
- Ubuntu/Debian security sources only decode the feeds; HTML-to-text,
  paragraph cleaning and classification run for advisories that touch a
  watched package. The Ubuntu notices response limit is 16 MiB (a page is
  about 3 MB), and `it-digest-watch.service` sets `MemoryMax=512M` with
  `GOMEMLIMIT=400MiB`, so a hostile or broken upstream cannot exhaust
  the server.
- `GITHUB_TOKEN` documentation: only public data is read, so a
  fine-grained token with no permissions is enough (`public_repo` is not
  needed).
- Deploy scripts quote the SSH host in every `ssh` call.
- `.claude/shared` submodule updated from d02ee2d to 4045037
  (claude-code-support-tools main). Every shared command that takes
  arguments now declares `argument-hint` (audit finding CC-001);
  `/commit-prepare` adds a mandatory uncached test gate on the exact tree
  to be committed; the Go toolchain hook also covers `go vet`, `make` and
  Go commands later in a command line, and warns when `jq` is missing;
  the code-quality command and agent gain untrusted-checkout guards.
- Removed Claude Code commands and an agent copied from another project
  (`.claude/commands/{build,clean,migrate,test}.md`,
  `.claude/agents/test-runner.md`): they described oCMS (`make assets`,
  goose migrations, `OCMS_SESSION_SECRET`), and `/clean` told the agent to
  delete databases. README and CLAUDE.md document initialising the
  `.claude/shared` submodule, without which the shared hooks do not run.
- `/commit-do` now refers to `/commit-prepare` (it named a nonexistent
  `/prepare-commit`), stages only the reviewed files by explicit path
  instead of `git add .`, and passes the message to `git commit -F` via a
  `mktemp` file so it never goes through the shell.
- Removed the unused `.claude/hooks/setup-go-toolchain.sh`, which ran
  `rm -rf /usr/local/go` and installed an unverified tarball from a
  predictable `/tmp` path.

### Added

- `digest watch` now posts PHP releases from php.net
  (`https://www.php.net/releases/active.php`): the latest release of every
  supported branch, recorded in `releases_seen` under package `php`. Posts
  show whether the release opens a new branch, updates the current branch
  or an older one, mark releases php.net tags as `security`, and link the
  announcement, ChangeLog and downloads. Implemented in
  `internal/phprelease`.
- `digest watch` now posts PHP security advisories, which the PHP project
  publishes as GitHub security advisories on `php/php-src`. Each new
  published, non-withdrawn advisory is recorded under package
  `php-security` (keyed by GHSA ID) and posted with severity, CVSS score,
  CVE, CWE, affected and fixed versions and a description excerpt.
  `GITHUB_TOKEN` is used when set, and only for `https://api.github.com`.
  Implemented in `internal/phpsec`.
- `digest watch` can alert on Ubuntu and Debian security advisories for
  the software your servers run (`internal/distrosec`). Opt-in per distro
  through `[ubuntu_security] packages` / `[debian_security] packages`:
  Ubuntu Security Notices for the configured release (default `noble`,
  24.04 LTS) and Debian Security Advisories (packages such as `php8.4`
  select Debian 13). An advisory is posted when it touches a watched
  source package, local issues included. Posts flag remotely exploitable
  issues, read from the advisory wording (kernel subsystem lists are not
  remote cues), and show impact, the update command and, for Ubuntu, CVEs
  and the fixed version.
  Advisories older than 7 days are ignored; there is no first-run
  baseline. `config-check` shows whether each source is enabled.
- Both PHP sources run in the same hourly run as the Go source and seed on
  their first run: the current state is recorded as the baseline and one
  "now tracked" notice is posted per source. Optional `[php] releases_url`
  and `advisories_url` overrides follow the existing feed URL rules and are
  printed by `digest config-check`.

### Security

- Shared HTTP client: a `Retry-After` longer than 60 seconds now ends the
  retries at once with the last error instead of being slept on. A hostile
  or broken upstream could previously stall `digest watch` for hours, so
  the next hourly run was skipped without any failure alert.
- `it-digest-watch.service` sets `TimeoutStartSec=10min`. A run that hangs
  anyway is stopped and marked failed, which fires the existing Telegram
  failure alert. Re-run `deploy/install.sh` to install the updated unit.
- PHP security advisories: control characters (ESC, NUL, BEL, …) and
  Unicode format characters (bidi overrides, zero-width characters) are
  removed from advisory text before rendering. They could otherwise reach
  terminals through `--dry-run` output, make Telegram reject the post on
  every run, or visually reorder text.
- PHP releases: versions with leading zeros are rejected, so `08.5.1`
  cannot become a second record of `8.5.1`, and a response listing more
  than 32 branches is treated as broken instead of producing a burst of
  posts.
- Daily digest: links in the model's blurbs stay live only when they point
  at the article's own host (`www.` ignored; other subdomains, other hosts,
  `javascript:`, relative and userinfo URLs are not). Other links keep only
  their text, and bare `http(s)://` URLs to other hosts, which Telegram
  would auto-link, are removed. Previously any link survived, so a prompt
  injection in a feed item could make the model post an arbitrary link to
  the channel. Code spans and formatting render as before. Links inside the
  headline become text, since the headline already links to the article
  and Telegram rejects nested links. The article link itself is used only
  when the item URL is an absolute `http(s)` URL of at most 512 bytes.
- Daily digest hardening:
  - Bare link tokens of any scheme (`tg://`, `ton://`, `ftp://`, …) and
    `www.` tokens are dropped unless they point at the article's host, also
    when glued to a preceding word (`_https://…`, `1https://…`). A token
    ends at characters that cannot appear unencoded in a URL, so a second
    URL glued onto an allowed one is checked on its own.
  - Filtering repeats until nothing more is removed, so removing a link
    cannot glue its neighbours into a new one
    (`tg:[//evil.example](https://x)`).
  - Headlines are capped at 300 bytes and blurbs at 700 before escaping.
    A source group that does not fit one message is split across messages
    (repeating the source name), and an entry that cannot fit even alone
    is skipped with a warning instead of failing the whole send.
  - Every entry is checked with the new `telegram.WellFormedMarkdownV2`
    and falls back to plain text if Telegram would reject it.
- MarkdownV2 escaping: an empty code span or fence is emitted as literal
  backticks, a link nested in a link's text becomes text, and a backslash
  before a line break no longer lets a link span lines. Each of these
  produced MarkdownV2 that Telegram rejects (for release notes, failing
  every hourly run until the upstream text changed). Output is otherwise
  unchanged. A fuzz test checks that every escaper yields well-formed
  output.
- Telegram `sendMessage` is no longer retried after a 5xx response or a
  dropped connection, when Telegram may already have posted the message;
  a retry posted it twice. Only HTTP 429 and failed connection setup (DNS,
  dial) are retried. An unsent release stays unrecorded and is posted on
  the next hourly run. New `httpx.RetryPolicy` (`WithRetryPolicy`,
  `SetRetryPolicy`); `telegram.New` installs `httpx.NotSentRetryable`.
- The shared HTTP client drops `X-Api-Key`-style headers on a redirect to
  another host (net/http only drops `Authorization` and `Cookie`), so the
  Anthropic API key cannot follow a redirect.
- Daily digest: a link on the article's host but another port
  (`good.com:8443`) is no longer kept live; default ports are ignored.
- Failure alerts posted to the public channel (no `admin_chat`) no longer
  include the server's hostname, and `digest config-check` warns when
  `admin_chat` is unset.
- `it-digest-backup.service` (`TimeoutStartSec=5min`) and
  `it-digest-notify@.service` (`2min`) now have start timeouts too.
- Sent messages are recorded even when the run is being stopped (SIGTERM
  from `systemctl stop`, a `TimeoutStartSec` expiry or a reboot). The writes
  after a successful send now use `store.AfterSend`, a context that is not
  cancelled with the run and is bounded to 5 seconds. Before, a stop
  between the send and the write lost the record and the next run posted
  the same release, seed notice or digest again.
- New `telegram.CleanText` removes control characters and Unicode format
  characters (bidi overrides, zero-width characters) from untrusted text.
  It is now used for PHP advisories, Drupal advisories (title, description,
  solution and fields: HTML character references such as `&#x1b;` decoded
  to real control characters), Go release-history text and the daily
  digest.
- `it-digest-daily.service` sets `TimeoutStartSec=30min`, so a hung digest
  run is stopped and reported through the failure alert. Re-run
  `deploy/install.sh` (or `make deploy`) to install the updated units.
- Dependabot (`.github/dependabot.yml`) opens one grouped PR per week for
  the SHA-pinned GitHub Actions and one for Go modules. It never bumps
  `modernc.org/libc` or `modernc.org/memory` on their own (they must match
  `modernc.org/sqlite`); `TestModerncPinnedToSQLite` fails any PR that
  gets them wrong.
- GitHub Actions are pinned to full commit SHAs (`actions/checkout`
  v6.1.0, `actions/setup-go` v6.5.0, `golangci/golangci-lint-action`
  v9.3.0) instead of movable tags.
- `golang.org/x/net` updated to `v0.62.0` (with `golang.org/x/sync`
  `v0.24.0`, `golang.org/x/sys` `v0.49.0` and `golang.org/x/text`
  `v0.43.0`), fixing two `x/net/html` vulnerabilities reachable from the
  release sources (govulncheck: GO-2026-6866 raw-text namespace check
  when rendering, reached via `gofeed` from the Drupal security feed
  parser; GO-2026-6860 quadratic text-node coalescing, reached from the
  Go release-notes summary). No Go toolchain change.

## [0.4.1] — 2026-10-21

### Fixed

- Drupal security feed HTTP 404 responses are retried after 10 and 30
  seconds before failing the watcher and triggering its failure alert.
  Other release sources and HTTP retry policies are unchanged.

## [0.4.0] — 2026-10-21

### Added

- `digest watch` now posts stable iOS and macOS releases from the Apple
  Developer releases feed
  (`https://developer.apple.com/news/releases/rss/releases.rss`). Only
  titles of the form `iOS 27.0.1 (24A446)` / `macOS Tahoe 26.1 (25B78)`
  match; betas, RCs, other Apple platforms and Xcode are ignored. Each new
  version is recorded in `releases_seen` under package `ios` / `macos` and
  posted with its build number, release-notes link and Apple's
  security-content index; updates to an older major branch are labelled as
  such. Each platform's first run records the feed's current releases as
  the baseline and posts one "now tracked" notice. New package
  `internal/applerelease`; optional `[apple_releases] feed_url` override,
  validated like `drupal_security.feed_url` and shown by `config-check`.
- CI runs `govulncheck` (pinned v1.9.0) in `.github/workflows/vulncheck.yml`
  on push/PR to `main` and every Monday, so newly published Go or
  dependency advisories fail the build. Locally: `make vulncheck`;
  `make install-tools` installs it.

### Changed

- `digest daily` supports `claude-sonnet-5-5` through schema-constrained
  JSON output, with no up-front thinking and medium effort. Recommended
  configuration now uses this model with `max_tokens = 2048`; explicit
  token budgets are preserved. Older models retain the forced-tool path.
- Anthropic requests log the model, elapsed time, and reported input and
  output token counts. Refused, truncated, missing, or malformed Sonnet
  5.5 output fails the run before posting or recording articles.
- `make install-tools` builds golangci-lint, gofumpt and govulncheck with
  the toolchain `go.mod` selects. `go install pkg@version` otherwise uses
  the tool's own `go.mod` and could pick an older Go that cannot load this
  module's packages.
- README: Telegram channel, latest release, Go version and license
  badges; the coverage table lists every package with current numbers.

### Fixed

- `it-digest-backup.sh` counts only the top-level database archives when
  reporting the backup size, so private rollback directories can no
  longer make a completed backup fail. An empty archive set reports zero
  while filesystem errors are still surfaced.

### Security

- Go updated to `1.27.3` and `golang.org/x/net` to `v0.60.0`, fixing
  standard-library and HTTP/2 vulnerabilities reachable from the HTTP
  client (govulncheck: GO-2026-6809 compromised x509 intermediates,
  GO-2026-6605 HTTP/1 client desync, GO-2026-6610 malformed HTTP/2
  framing headers, GO-2026-6608 MIME header memory limit bypass, and
  others). Build environments now require Go 1.27.3.
- The shared HTTP client (`httpx.New`) refuses redirects from `https` to
  plain `http`, for every caller (release sources, feeds, Telegram,
  Anthropic). Previously Go's default policy followed them, which would
  resend the request — including, on a same-host redirect, the GitHub
  `Authorization` header — unencrypted, and let a network attacker serve
  forged feed content. A refused redirect fails immediately with
  `httpx.ErrInsecureRedirect` (no retries), and its error text goes through
  the client's URL sanitizer. The 10-redirect limit is unchanged.
- Network-failure errors from `httpx.Client.Do` (DNS failure, refused
  connection, timeout, context cancel) no longer leak the raw request URL.
  net/http's `*url.Error` text embeds it, which put the Telegram bot token
  (`/bot<TOKEN>/`) into the journal whenever Telegram was unreachable; its
  URL now goes through the client's sanitizer, as the outer message's
  already did.

## [0.3.0] — 2026-10-01

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

#### Toolchain and dependencies

- Go updated to `1.27.1`; build environments now require that version
  or newer. Updated `gofeed` to `v1.5.0`, `golang.org/x/net` to
  `v0.59.0`, `golang.org/x/sync` to `v0.23.0`, and
  `modernc.org/sqlite` to `v1.60.1`, with compatible indirect
  dependencies. Removed dependencies no longer needed by `gofeed`.
  The database schema is unchanged.
- Developer tools pinned to `golangci-lint v2.14.0` and
  `gofumpt v0.12.0`; CI uses the same lint version.

#### Deployment

- `make deploy` now syncs systemd units as well as the binary,
  reloads systemd when units change, and restarts changed timers
  so updated schedules take effect.

#### Daily digest

- The Anthropic `/v1/messages` request now defaults to a 120-second
  timeout instead of the generic 30-second `httpx` timeout. Large
  daily-digest prompts can legitimately exceed 30 seconds; the new
  optional `llm.timeout_seconds` setting makes the budget explicit
  without changing feed or Telegram clients.

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

#### Systemd scheduling and alerts

- The watcher uses `OnCalendar=hourly` with `Persistent=true` so
  its one-shot service runs hourly and catches up after downtime.
- Moved `OnFailure` into the `[Unit]` sections of the watcher and
  daily services so failures trigger the Telegram notifier.

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

[Unreleased]: https://github.com/olegiv/it-digest-bot/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/olegiv/it-digest-bot/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/olegiv/it-digest-bot/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/olegiv/it-digest-bot/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/olegiv/it-digest-bot/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/olegiv/it-digest-bot/releases/tag/v0.1.0
