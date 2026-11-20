# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common commands

```bash
make help          # show Makefile targets
make all           # build the default local/dev binary
make build         # fast local/dev build to bin/digest
make build-prod    # optimized host production build to bin/digest
make build-linux-amd64  # optimized static linux/amd64 production build
make build-darwin-arm64 # optimized darwin/arm64 production build
make build-all-platforms # linux/amd64 + darwin/arm64 production builds
make test          # go test ./...
make test-race     # go test -race ./...
make coverage      # go test -cover ./...
make coverage-html # write coverage.out + coverage.html
make fmt           # gofumpt -w .
make fmt-check     # fail if gofumpt would reformat files
make vet           # go vet ./...
make lint-go       # golangci-lint run ./... (config: .golangci.yml)
make lint          # run all linters
make vulncheck     # govulncheck ./... (known vulns reachable from the code; needs network)
make check         # fmt-check + vet + lint + test
make deps          # go mod download
make tidy          # go mod tidy
make install-tools # install golangci-lint v2.14.0 + gofumpt v0.12.0 + govulncheck v1.9.0 (pinned)
make run-watch     # go run ./cmd/digest watch --config config.toml
make run-dry       # render a fake release post to stdout (no network)
```

Single test / package:
```bash
go test ./internal/digest/...                    # one package
go test -run TestRenderAndSplit ./internal/digest # one test
go test -race -run TestWatcher_Run ./internal/claudecode
```

CI (`.github/workflows`) runs build + vet + race + lint on push/PR to `main`; `vulncheck.yml` runs govulncheck on push/PR to `main` and weekly.

## Architecture

`it-digest-bot` is a single static Go binary (`cmd/digest`) dispatched by **systemd timers** on Ubuntu — there is no daemon, no HTTP server, no Docker. Every subcommand is a one-shot run that exits when done; idempotency comes from the SQLite store, not in-process state.

Two flows, each driven by its own systemd unit pair under `deploy/systemd/`:

1. **`digest watch`** (hourly) — `internal/releasewatch.Runner` executes release sources and handles the shared seen-check, dry-run, Telegram send, and audit-log flow. `internal/claudecode.Source` queries npm `dist-tags.latest` for `@anthropic-ai/claude-code`, skips already-posted `(package, version)` rows, then requires GitHub `/releases/latest` to name the same version before posting. `internal/gorelease.Source` queries `https://go.dev/dl/?mode=json` and announces every unseen stable Go version returned there. `internal/phprelease.Source` reads `https://www.php.net/releases/active.php` (latest release per active branch) and posts each unseen branch tip under package `php`, flagging releases tagged `security`; `internal/phpsec.Source` reads the GitHub repository security advisories of `php/php-src` (one page of 100, newest published first) and posts each unseen published, non-withdrawn advisory under package `php-security` keyed by GHSA ID, sending `GITHUB_TOKEN` only when the endpoint is `https://api.github.com`. Both are `Seeder`s, so their first run records the current state and posts one notice each. `internal/distrosec` adds two opt-in sources, enabled by the `packages` lists in `[ubuntu_security]` / `[debian_security]`: Ubuntu Security Notices from `ubuntu.com/security/notices.json` (package `usn-<release>`) and Debian Security Advisories from `www.debian.org/security/dsa-long.rdf` (package `dsa`). An advisory is posted when it touches a watched source package, is not limited to local attackers (judged per description paragraph from the teams' wording, see `distrosec.Assess`) and is at most 7 days old. They are deliberately not `Seeder`s: a filtered list is often empty, and seeding on the first non-empty run would swallow a real alert. `internal/drupalsec.Source` fetches the combined drupal.org security RSS feed (core + contrib + PSA), parses each advisory (ID, project, risk, affected versions, CVEs, solution) and posts every unseen one oldest-first under package `drupal-security`; it implements the optional `releasewatch.Seeder` hook, so on the very first run (no `releases_seen` row for its package) the runner records the feed's current items (50 at the time of writing) as seen and posts one notice instead of one announcement each. `internal/applerelease.Source` (one instance per platform in `applerelease.Platforms`: iOS, macOS) reads the Apple Developer releases RSS, keeps only stable titles (`iOS 27.0.1 (24A446)`, `macOS Tahoe 26.1 (25B78)` — betas/RCs/other OSes never match the anchored regex) and posts each unseen version under package `ios` / `macos`, flagging older-major-branch updates; it is a `Seeder` too, so each platform's first run records the feed window and posts one notice. Re-running is safe; already-seen and deferred candidates do no Telegram or DB writes. `Runner.MaxPostsPerRun` (default 10) caps send attempts per run across all sources (seeding excluded); capped candidates stay unrecorded and are posted on later runs.

2. **`digest daily`** (08:00 Europe/Zurich) — `internal/digest/Builder`: `errgroup` parallel-fetch of all `[[feed]]` entries → dedupe via `articles_seen.url_hash` (SHA-256 of canonicalized URL) → send the 24h window to Anthropic `/v1/messages` for ranking + summarization → render MarkdownV2 grouped by source → split into chunks under `telegram.MaxMessageBytes` (4096) → post each chunk → record per chunk.

Both flows share `internal/store` (SQLite via `modernc.org/sqlite`, no CGO), `internal/telegram`, and `internal/httpx`. The store opens with `journal_mode=WAL`, `busy_timeout=5000`, and **`SetMaxOpenConns(1)`** — SQLite serializes writes; do not raise this. Schema lives in `migrations/*.sql` (embedded `embed.FS`, applied by `digest migrate`).

### Package boundaries

- `internal/releasewatch` — shared release watcher runner: source candidates, seen checks, dry-run output, Telegram send, and post logging.
- `internal/claudecode` — Claude Code release source/client: `npm.go`, `github.go`, `changelog.go`, `format.go`, `source.go`, `watcher.go`.
- `internal/gorelease` — official Go stable release source/client from go.dev downloads + release history.
- `internal/applerelease` — Apple Developer releases RSS source for stable iOS/macOS: feed client + title/notes-link parser (`client.go`), MarkdownV2 formatter (`format.go`), per-platform `Source` + `Seeder` (`source.go`). No LLM.
- `internal/phprelease` — php.net active-branches JSON client + parser (`client.go`), MarkdownV2 formatter (`format.go`), `Source` + `Seeder` (`source.go`). Post links are rebuilt from the validated version, never taken from the response. No LLM.
- `internal/phpsec` — GitHub `php/php-src` security advisories client + parser (`client.go`), MarkdownV2 formatter with capped, plain-escaped fields (`format.go`), `Source` + `Seeder` (`source.go`). No LLM.
- `internal/distrosec` — Ubuntu USN JSON client (`ubuntu.go`), Debian DSA RDF client (`debian.go`), exposure/impact classification (`classify.go`), MarkdownV2 formatter with capped, plain-escaped fields (`format.go`), filtering `Source` (`source.go`). No LLM.
- `internal/drupalsec` — drupal.org security advisory source: feed client (`gofeed`), HTML field parser (`client.go`), MarkdownV2 formatter (`format.go`), `Source` + `Seeder` (`source.go`). No LLM.
- `internal/digest` — phase 2 orchestrator + `render.go` (MarkdownV2 layout + chunk splitter).
- `internal/news` — feed fetch (`gofeed`) + canonical URL hashing.
- `internal/llm` — `Summarizer` interface (`anthropic.go` is the prod impl, mockable in tests).
- `internal/telegram` — Bot client + `markdownv2.go` escape helpers (Telegram MarkdownV2 escaping is fiddly; use these helpers, do not hand-escape) + `cleantext.go`. Text from any upstream (feeds, scraped HTML, APIs, LLM output) goes through `CleanText` (drops control and bidi/zero-width characters, collapses whitespace) and then `EscapeMarkdownV2Plain`, so it can never become live markup; `EscapeMarkdownV2` is only for trusted release-note prose. The daily digest is the exception: headlines and blurbs keep their formatting via `EscapeMarkdownV2Links`, whose link policy (`digest.sameHost`) keeps a link live only when it points at the article's own host and drops off-host bare link tokens; each entry is length-capped and checked with `WellFormedMarkdownV2`, falling back to plain text. Any new escaper output must stay well formed (`FuzzEscapersWellFormed`).
- `internal/store` — `Releases`, `Articles`, `Posts` repositories on `*sql.DB`. Writes that record an already-sent Telegram message use `store.AfterSend(ctx)` (not cancelled by SIGTERM, bounded to 5 s) so a stopped run never reposts.
- `internal/config` — TOML loader; secrets are **never** read from TOML.

### Dry-run pattern

`releasewatch.Runner`, `claudecode.Watcher`, and `digest.Builder` expose `DryRun bool` + `DryOut io.Writer` fields. When `DryRun=true` they print rendered output to `DryOut` (defaults to `os.Stdout`) and skip **both** the Telegram send **and** all DB writes — making the same run repeatable. A first-run seed (see `releasewatch.Seeder`) prints a `SEED <source>` block listing what would be recorded plus the notice text, and likewise writes nothing. Preserve this contract when modifying them.

### URL-sanitizer contract

Both `telegram.New` and `news.NewFeedFetcher` unconditionally install their own `SanitizeURL` on the `*httpx.Client` they receive — masking `/bot<TOKEN>/` in the Telegram case, stripping userinfo/query/fragment in the feed-fetcher case. This covers retry-exhausted errors from `httpx.Client.Do` where a naive `url.URL.Redacted()` would leak the token in the path or an API key in the query string. Because this **mutates** the injected client, `cmd/digest/watch.go` and `cmd/digest/daily.go` construct separate `httpx.Client`s per API (`apiHTTP`, `feedHTTP`, `tgHTTP`) so one constructor's sanitizer does not stomp another's. `telegram.New` also installs `httpx.NotSentRetryable` on its client: `sendMessage` is not idempotent, so it is retried only on 429 or a failed connection setup, never after a 5xx or a dropped connection. If you add a new caller of `telegram.New` or `news.NewFeedFetcher`, give it its own `httpx.New()` — do not share.

### Secrets and config

- `config.toml` (TOML) holds non-secret settings. See `config.example.toml`.
- Secrets come from env only: `TELEGRAM_BOT_TOKEN` (always), `ANTHROPIC_API_KEY` (`digest daily`), `GITHUB_TOKEN` (optional — raises GitHub rate limit).
- `lookback_hours` defaults to 48 (not 24) because some feeds stamp items at 00:00 UTC; dedup against `articles_seen` makes the wider window safe.

## Conventions (enforced)

- `log/slog` only — no `fmt.Println` or `log.Printf` in non-test code.
- Wrap errors with `fmt.Errorf("...: %w", err)`; never bare-return external errors.
- Thread `context.Context` through every I/O function.
- `gofumpt` (stricter than `gofmt`) is the formatter; `goimports` orders imports.
- `.golangci.yml` enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `gosec`, `revive`, `gocritic`. `gosec` excludes G101/G304/G404 with documented reasons in the config — match that style if adding more excludes.
- Test coverage target: ≥ 70% on `internal/claudecode`, `internal/telegram`, `internal/config` (currently all packages exceed this).
- Never add "Generated by Claude Code" (or any similar AI-generated footer) or `Co-Authored-By:` trailers to commit messages, PR descriptions, release notes, issues or comments.

## Shared Claude Code tooling

Shared agents, commands and hooks live in the `olegiv/claude-code-support-tools` git submodule at `.claude/shared` and surface through relative symlinks under `.claude/agents/`, `.claude/commands/` and `.claude/hooks/` (the Go toolchain and `go test` hooks in `.claude/settings.json`). Run `git submodule update --init` after cloning, or the symlinks dangle and the hooks silently do nothing. Treat the symlinked files as read-only — edits belong upstream; bump the pinned commit with `git submodule update --remote .claude/shared` and commit the new pointer. `.claude/settings.local.json` is personal and gitignored.

## Deploy

Production install is `scp` + `deploy/install.sh` (no goreleaser, no release artifacts). The user deploys manually — do not add release-asset workflows.
