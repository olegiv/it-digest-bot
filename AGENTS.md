# AGENTS.md

This file provides guidance to AI coding agents (Codex, Aider, Cursor, etc.) when working with code in this repository.

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
make check         # fmt-check + vet + lint + test
make deps          # go mod download
make tidy          # go mod tidy
make install-tools # install golangci-lint v2.11.4 + gofumpt v0.9.2 (pinned)
make run-watch     # go run ./cmd/digest watch --config config.toml
make run-dry       # render a fake release post to stdout (no network)
```

Single test / package:
```bash
go test ./internal/digest/...                    # one package
go test -run TestRenderAndSplit ./internal/digest # one test
go test -race -run TestWatcher_Run ./internal/claudecode
```

CI (`.github/workflows`) runs build + vet + race + lint on push/PR to `main`.

## Architecture

`it-digest-bot` is a single static Go binary (`cmd/digest`) dispatched by **systemd timers** on Ubuntu — there is no daemon, no HTTP server, no Docker. Every subcommand is a one-shot run that exits when done; idempotency comes from the SQLite store, not in-process state.

Two flows, each driven by its own systemd unit pair under `deploy/systemd/`:

1. **`digest watch`** (hourly) — `internal/releasewatch.Runner` executes release sources and handles the shared seen-check, dry-run, Telegram send, and audit-log flow. `internal/claudecode.Source` queries npm `dist-tags.latest` for `@anthropic-ai/claude-code`, skips already-posted `(package, version)` rows, then requires GitHub `/releases/latest` to name the same version before posting. `internal/gorelease.Source` queries `https://go.dev/dl/?mode=json` and announces every unseen stable Go version returned there. Re-running is safe; already-seen and deferred candidates do no Telegram or DB writes.

2. **`digest daily`** (08:00 Europe/Zurich) — `internal/digest/Builder`: `errgroup` parallel-fetch of all `[[feed]]` entries → dedupe via `articles_seen.url_hash` (SHA-256 of canonicalized URL) → send the 24h window to Anthropic `/v1/messages` for ranking + summarization → render MarkdownV2 grouped by source → split into chunks under `telegram.MaxMessageBytes` (4096) → post each chunk → record per chunk.

Both flows share `internal/store` (SQLite via `modernc.org/sqlite`, no CGO), `internal/telegram`, and `internal/httpx`. The store opens with `journal_mode=WAL`, `busy_timeout=5000`, and **`SetMaxOpenConns(1)`** — SQLite serializes writes; do not raise this. Schema lives in `migrations/*.sql` (embedded `embed.FS`, applied by `digest migrate`).

### Package boundaries

- `internal/releasewatch` — shared release watcher runner: source candidates, seen checks, dry-run output, Telegram send, and post logging.
- `internal/claudecode` — Claude Code release source/client: `npm.go`, `github.go`, `changelog.go`, `format.go`, `source.go`, `watcher.go`.
- `internal/gorelease` — official Go stable release source/client from go.dev downloads + release history.
- `internal/digest` — phase 2 orchestrator + `render.go` (MarkdownV2 layout + chunk splitter).
- `internal/news` — feed fetch (`gofeed`) + canonical URL hashing.
- `internal/llm` — `Summarizer` interface (`anthropic.go` is the prod impl, mockable in tests).
- `internal/telegram` — Bot client + `markdownv2.go` escape helpers (Telegram MarkdownV2 escaping is fiddly; use these helpers, do not hand-escape).
- `internal/store` — `Releases`, `Articles`, `Posts` repositories on `*sql.DB`.
- `internal/config` — TOML loader; secrets are **never** read from TOML.

### Dry-run pattern

`releasewatch.Runner`, `claudecode.Watcher`, and `digest.Builder` expose `DryRun bool` + `DryOut io.Writer` fields. When `DryRun=true` they print rendered output to `DryOut` (defaults to `os.Stdout`) and skip **both** the Telegram send **and** all DB writes — making the same run repeatable. Preserve this contract when modifying them.

### URL-sanitizer contract

Both `telegram.New` and `news.NewFeedFetcher` unconditionally install their own `SanitizeURL` on the `*httpx.Client` they receive — masking `/bot<TOKEN>/` in the Telegram case, stripping userinfo/query/fragment in the feed-fetcher case. This covers retry-exhausted errors from `httpx.Client.Do` where a naive `url.URL.Redacted()` would leak the token in the path or an API key in the query string. Because this **mutates** the injected client, `cmd/digest/watch.go` and `cmd/digest/daily.go` construct separate `httpx.Client`s per API (`apiHTTP`, `feedHTTP`, `tgHTTP`) so one constructor's sanitizer does not stomp another's. If you add a new caller of `telegram.New` or `news.NewFeedFetcher`, give it its own `httpx.New()` — do not share.

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

## Deploy

Production install is `scp` + `deploy/install.sh` (no goreleaser, no release artifacts). The user deploys manually — do not add release-asset workflows.
