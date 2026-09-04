# Phase 0 — Repository bootstrap

**Status: DONE** (scaffolded for you — this phase is pure boilerplate, not a
learning target). Read it so you know what's here before Phase 1.

## Objective

A clean Go skeleton that builds and runs, plus the docs structure that holds your
learning notes. No metrics — this phase exists so Phase 1's diff is pure API code
and Phase 2's diff is pure instrumentation.

## Concepts

- **`cmd/` vs `internal/`** — `cmd/api` is the entrypoint; `internal/` is a
  compiler-enforced privacy boundary (no other module can import it).
- **Graceful shutdown** — a server killed mid-request drops connections. We catch
  SIGINT/SIGTERM, stop accepting new connections, and drain in-flight requests up
  to a timeout. This also matters for the lab: Phase 3 tests `up` by stopping this
  process cleanly.
- **Bounded drain** — `srv.Shutdown(ctx)` with no deadline hangs forever on a
  stuck request. Always pass a timeout context (`API_SHUTDOWN_TIMEOUT`, default 10 s).
- **`http.ErrServerClosed`** — `ListenAndServe` always returns a non-nil error;
  this specific one is the *expected* result of `Shutdown` and must not be treated
  as a failure.

## What was built

```
cmd/api/main.go            server bootstrap, timeouts, signal-aware shutdown
internal/api/router.go     NewRouter() -> ServeMux with GET /health
internal/api/router_test.go  /health 200 + JSON; unknown route 404
internal/config/config.go  env-based config (API_ADDR, API_SHUTDOWN_TIMEOUT)
Makefile                   run / build / test / tidy / vet / fmt / check
.editorconfig .golangci.yml .gitignore
docs/roadmap.md 00-intro.md glossary.md
```

### Server timeouts (in `main.go`)

| Field | Value | Why |
|-------|-------|-----|
| `ReadHeaderTimeout` | 5 s | Slowloris guard; gosec G112 flags its absence |
| `ReadTimeout` | 15 s | Whole request must arrive in time |
| `WriteTimeout` | 30 s | Bounds slow responses (`/slow` sleeps up to 2 s, so headroom) |
| `IdleTimeout` | 60 s | Keep-alive connection reuse window |

## Definition of Done

- [x] `make run` serves `GET /health` → `{"status":"ok","uptime":"..."}` with
      `Content-Type: application/json`
- [x] `make build` produces `./bin/api`
- [x] `make test` passes; `go vet ./...` clean
- [x] Ctrl-C logs `shutting down` → `stopped`, no panic
- [x] README has the full phase checklist; `docs/` has intro, glossary, roadmap
- [ ] Committed and tagged `phase-0`

## Verify it yourself

```sh
make check          # fmt + vet + test
make run &
curl -i localhost:8080/health
kill %1             # watch the graceful-shutdown logs
```

## What was deliberately left out

- No router library, no config library, no logging library beyond stdlib `slog`.
- No metrics, no Docker, no `/users` etc. — all Phase 1+.
