# Configuration

Config is **env-vars only** — no YAML/JSON/TOML file is parsed by the Go binary.
`internal/config/config.go` reads `os.Getenv` with hardcoded defaults; see its
doc comment for the full list of keys.

`// ponytail:` one struct + `os.Getenv`, no config library. A `.env` *file* is
just a convenient way to set those same env vars — it changes nothing on the
Go side.

## Precedence (highest wins)

1. A var already exported in your shell (`API_ERROR_RATE=1 make run`)
2. A var set in the loaded env file (`.env` by default)
3. The default baked into `config.Load()`

## Local dev — one file, `.env`

```sh
cp .env.example .env   # edit as needed; .env is gitignored, yours only
make run                # loads .env automatically if it exists
```

`.env.example` is the **committed source of truth** for every tunable and its
default — read it, don't guess env var names from the code.

## Multiple profiles

`make run` accepts `ENV_FILE` to point at a different file instead of `.env`:

```sh
ENV_FILE=.env.chaos make run
```

There's no `.env.chaos` yet — Phase 6 (failure simulation) is where a second
profile earns its place, e.g. an "incident" preset with `API_ERROR_RATE=0.8`.
Add the file when that phase needs it; don't pre-create profiles nobody reads
yet.

## Docker Compose (Phase 3+)

Compose reads the **same root `.env`** automatically for `${VAR}` substitution
in `docker-compose.yml`, and each service can also declare `env_file: .env` to
inject those vars into its container. One file serves both `make run` and
`docker compose up` — no duplication between local and containerized config.
