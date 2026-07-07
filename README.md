# dreamreader-sync

Standalone cloud-sync backend for the **Dream Manga Reader**（梦漫）app.

It is a small, self-contained service that stores **one sync document per user**
— settings, sources, reading history, favorites, whatever the client packs into
a JSON blob — and hands it back on another device. It is *not* part of the
main-site (hertz-community) stack; its only tie to the ecosystem is that
**accounts come from hertz-iam**: the app logs into IAM, gets an access token,
and this service validates that token via IAM's JWKS. It never reads IAM storage.

- **Language / storage:** Go + SQLite (CGO-free `modernc.org/sqlite`) — one
  static binary + one `.db` file. No separate database container.
- **Auth:** OIDC resource server. Bearer access token, RS256, validated against
  the IAM JWKS. Audience must equal the app's `client_id` (fail-closed).
- **Concurrency:** content-derived `ETag` + `If-Match` optimistic concurrency, so
  a stale device can never clobber newer data — it gets a `409` with the current
  server document and merges client-side.

## API

| Method | Path             | Auth | Purpose |
|--------|------------------|------|---------|
| `GET`  | `/healthz`       | no   | liveness probe |
| `GET`  | `/api/v1/sync`   | yes  | fetch the caller's document |
| `PUT`  | `/api/v1/sync`   | yes  | store the caller's document |

Responses use the ecosystem envelope: `{"code":0,"msg":"ok","data":...}`.

**`GET /api/v1/sync`** → `data: { doc: <json|null>, etag: "<hex>", updated_at }`.
`doc` is `null` and `etag` is `""` before the first push.

**`PUT /api/v1/sync`** — body is the opaque JSON document. Send the last ETag you
saw in `If-Match` (omit it for the first-ever push):

- match → `200`, `data: { etag, updated_at }` (and an `ETag` response header).
- stale → `409`, `data: { doc, etag, updated_at }` — the current server state to
  merge against, then retry with the new ETag.

The normal client loop is **pull → merge locally → push with If-Match**; on `409`
re-pull, re-merge, retry.

## Configuration (env)

| Variable | Default | Notes |
|----------|---------|-------|
| `DREAMSYNC_HTTP_ADDR` | `:8090` | listen address |
| `DREAMSYNC_DB_PATH` | `./data/dreamsync.db` | SQLite file (back up this file) |
| `DREAMSYNC_IAM_ISSUER` | `http://localhost:8080` | expected token issuer |
| `DREAMSYNC_JWKS_URI` | *(issuer)* `/realms/user/jwks` | provider JWKS |
| `DREAMSYNC_CLIENT_ID` | `dreamreader` | mandatory token audience |
| `DREAMSYNC_MAX_DOC_BYTES` | `8388608` | per-document upload cap (8 MiB) |
| `DREAMSYNC_CORS_ORIGINS` | *(none)* | comma-separated web-origin allowlist |

## Run locally

```sh
go test ./...
go run ./cmd/dreamreader-sync          # needs a reachable IAM for real tokens
```

## Docker

The build is self-contained (the IAM validator is vendored under
`internal/authmw`) — no sibling repos, any checkout name works:

```sh
docker build -t dreamreader-sync .
```

Or via compose (`deploy/dev/docker-compose.yml`):

```sh
cd deploy/dev
docker compose up --build
```

## Layout

```
cmd/dreamreader-sync/   entrypoint (config → store → validator → server)
internal/config/        env-driven configuration
internal/store/         SQLite store: sync_docs, ETag optimistic concurrency
internal/httpapi/       routing, IAM auth, CORS, sync handlers, integration test
internal/authmw/        vendored IAM token validator (middleware + jwks)
Dockerfile              CGO-free, self-contained static build → alpine
deploy/                 dev + install (interactive) deployment
```
