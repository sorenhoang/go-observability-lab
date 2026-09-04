# Phase 1 — Simple Go API

**Status: TODO — you code this manually, then ask for a review.**

## Objective

The intentionally boring API. Six endpoints, in-memory fake data, no metrics.
Keeping metrics out means Phase 2's diff is *pure instrumentation* — you'll see
exactly what it takes to make a plain handler observable.

## Concepts to internalize

- **`ServeMux` method + pattern routing** (Go 1.22+): `mux.HandleFunc("GET /users", ...)`.
  Wrong method on a known path → 405 automatically. Unknown path → 404.
- **Route template vs raw path** — `/users` is the template; `/users?page=2` and
  (later, if you had them) `/users/42` are raw paths. **Metrics must be labelled by
  template**, or every distinct path becomes its own time series (cardinality
  explosion — the headline mistake this lab teaches). You build the helper now so
  Phase 2 just calls it.
- **Validation at the boundary** — the handler is the trust boundary. Validate
  `qty > 0` and "product exists" there; return 400 / 404 with a JSON error body.
- **Deterministic-ish fault endpoints** — `/slow` and `/error` are *simulators*.
  They need to be tunable (query param + env) so Phase 6 can dial them.

## What to build

### 1. `routePattern` helper — `internal/api/router.go`

```go
// routePattern returns the matched route template for a request, e.g. "/users".
// Falls back to "other" when nothing matched, so the metrics label set in
// Phase 2 stays bounded.
func routePattern(r *http.Request) string {
    if r.Pattern == "" {
        return "other"
    }
    // r.Pattern is like "GET /users"; strip the method.
    _, path, ok := strings.Cut(r.Pattern, " ")
    if !ok {
        return "other"
    }
    return path
}
```

> `http.Request.Pattern` is populated by `ServeMux` in Go 1.23+. Confirm it's set
> for your routes with a quick test — that test is also your DoD evidence.

### 2. Fake data + handlers — `internal/api/handlers.go`

In-memory slices, package-level, no mutex needed for reads:

```go
type user struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}
type product struct {
    ID    int     `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}
var users = []user{ {1,"Ada"}, {2,"Alan"}, {3,"Grace"} }
var products = []product{ {1,"Widget",9.99}, {2,"Gadget",19.99}, {3,"Gizmo",4.50} }
```

| Handler | Behavior |
|---------|----------|
| `GET /health` | already done — leave it |
| `GET /users` | 200, JSON array of `users` |
| `GET /products` | 200, JSON array of `products` |
| `POST /orders` | parse `{"product_id":int,"qty":int}`; `qty<=0` → 400; product not found → 404; else 201 with `{"order_id":<n>,"product_id":...,"qty":...}` |
| `GET /slow` | sleep `rand` in `[minMs, maxMs]`, then 200; `?max_ms=` overrides max |
| `GET /error` | with probability `p` return 500 `{"error":"simulated failure"}`, else 200; `?rate=` overrides `p` |

Order IDs: a package-level `atomic.Int64` counter is enough. `POST /orders`
mutates it — that's the only write, and `atomic` covers it. (No need for a store
abstraction; a real DB lands in Phase 8.)

### 3. Config additions — `internal/config/config.go`

```go
SlowMinMs   int      // API_SLOW_MIN_MS   default 50
SlowMaxMs   int      // API_SLOW_MAX_MS   default 2000
ErrorRate   float64  // API_ERROR_RATE    default 0.3
```

Pass `Config` into `NewRouter(cfg)` (or a small `Handlers` struct) so handlers
read tunables from there, not from `os.Getenv` directly. Query params override
per-request.

### 4. `docs/01-api.md`

Table of endpoints, their behavior, and the tunables. One paragraph on why
`/slow` and `/error` exist (production-problem simulators for later phases).

### 5. Tests — `internal/api/handlers_test.go`

Table tests. Cover every branch:

- `/users`, `/products` → 200 + correct length
- `POST /orders`: valid → 201; `qty:0` → 400; `product_id:999` → 404; malformed JSON → 400
- `/slow?max_ms=10` → 200 and returns in well under a second
- `/error?rate=1` → always 500; `?rate=0` → always 200
- `routePattern` returns `/users` for a `/users` request, `other` for `/nope`

Use `httptest.NewServer` (or `httptest.NewRecorder` + the mux directly).

## Definition of Done

- [ ] All six routes reachable; unknown path → 404; wrong method → 405
- [ ] `POST /orders` validation: 201 / 400 / 404 correct; malformed body → 400
- [ ] `/slow` latency visibly varies; `?max_ms=` caps it
- [ ] `/error` 500-rate ≈ configured probability over 100 calls; `?rate=` overrides
- [ ] `routePattern` returns the **template**, never the raw path; `other` fallback works
- [ ] Table tests green; `make check` clean
- [ ] `docs/01-api.md` written
- [ ] Committed, tagged `phase-1`

## Traps to hit on purpose

- Try labelling something by `r.URL.Path` in your head and note why `/orders` vs
  `/orders?x=1` would be two series. Phase 2 will make this real.
- Notice `/error` returning 500 is a *counter* concern (rate of errors), while
  "how many requests are in flight right now" is a *gauge* concern. Phase 2 splits
  these.

## Deliberately out of scope

- No database, no ORM, no auth, no pagination, no request logging middleware.
- No metrics — resist the urge.
