# Deploy Pesenin on Vercel (DEV)

One Vercel project serves the SvelteKit PWA and the Go API on the same origin. Browser calls stay on `/api/...`. Server-rendered pages call the API through a service binding (`API_INTERNAL_URL`).

[Vercel Services](https://vercel.com/docs/services) and the [Go runtime](https://vercel.com/docs/functions/runtimes/go) are in beta on all plans. `docker compose up --build` is unchanged and still listens on http://localhost:43123.

## Project settings

Create one project from this repository. Leave the root at the repository root so `vercel.json` is picked up. Do not set the root directory to `frontend` or `backend`.

| Setting | Value |
| --- | --- |
| Root Directory | repository root (leave empty) |
| Framework Preset | **Other** |
| Build Command | empty (each service builds itself) |
| Output Directory | empty |
| Install Command | empty (the `web` service runs `npm ci`) |
| Node.js Version | 24.x |

`vercel.json` defines two services:

| Service | Root | Framework | Build |
| --- | --- | --- | --- |
| `web` | `frontend` | SvelteKit | `npm run build` via the SvelteKit preset. On Vercel, `VERCEL=1` selects `@sveltejs/adapter-vercel` (Node.js 24). Docker Compose still builds with `@sveltejs/adapter-node`. |
| `api` | `backend` | Go | `go build -o server ./cmd/api` |

Public routing, in order:

1. `/api/(.*)` → Go API (paths are unchanged, so the existing chi routes match)
2. `/(.*)` → SvelteKit

The `web` service binds to `api` and receives the internal URL as `API_INTERNAL_URL`. Do not set that variable yourself.

In the dashboard, set the function region next to the Neon region (Project Settings → Functions). With Fluid Compute, memory is a dashboard setting, not a `vercel.json` key.

## Database

Attach **Neon Postgres** from the Vercel Marketplace to this project, and include the Preview environment (and a custom environment, if you use one). The integration injects:

| Variable | Use |
| --- | --- |
| `DATABASE_URL` | Pooled URL. The API uses it at runtime. A host containing `-pooler` is treated as PgBouncer transaction mode (no prepared-statement cache, pool capped at 4 connections on Vercel). |
| `DATABASE_URL_UNPOOLED` | Direct URL. Migrations use this when it is set. `POSTGRES_URL_NON_POOLING` is accepted as an alias. |

The API applies the embedded SQL migrations on startup, same as the Compose binary. To run them yourself against Neon:

```bash
cd backend
go run ./cmd/api migrate
```

That reads `DATABASE_URL_UNPOOLED` when it is set, otherwise `POSTGRES_URL_NON_POOLING`, otherwise `DATABASE_URL`. From a laptop, export the direct URL first:

```bash
cd backend
DATABASE_URL='postgresql://USER:PASSWORD@ep-example.region.aws.neon.tech/neondb?sslmode=require' \
  go run ./cmd/api migrate
```

Use the direct host (no `-pooler`) for that command. `sslmode=require` is required by Neon. A successful run logs `migrations applied` and exits 0. `ErrNoChange` is success.

Pull the project env into a local file before running the command from a shell that does not already have the variables:

```bash
npx vercel env pull .env.vercel --environment=preview
set -a && . ./.env.vercel && set +a
cd backend && go run ./cmd/api migrate
```

Do not commit `.env.vercel`.

## Environment variables

Set these on **Preview** for the DEV deployment. Add the same keys to a custom environment if you create one. Production can reuse the list with production values when you are ready.

Mark secrets as Sensitive. There is no session-signing secret: a session is a random token stored as a hash in Postgres (`SESSION_DAYS`, default 30).

| Variable | DEV value | Required |
| --- | --- | --- |
| `DATABASE_URL` | Neon pooled URL. Injected by the Marketplace integration. | yes |
| `DATABASE_URL_UNPOOLED` | Neon direct URL. Injected by the integration. Used for migrations. | yes for migrations through PgBouncer |
| `PUBLIC_ORIGIN` | `https://<dev-host>` with no trailing slash. Use the stable branch alias or custom domain, not the per-deployment URL. | yes for Google sign-in |
| `GOOGLE_CLIENT_ID` | OAuth web client id | yes, unless you only use a private demo |
| `GOOGLE_CLIENT_SECRET` | OAuth client secret. Sensitive. | yes, with the client id |
| `GOOGLE_REDIRECT_URL` | `https://<dev-host>/api/v1/auth/google/callback`. Defaults to `{PUBLIC_ORIGIN}/api/v1/auth/google/callback` when unset. | recommended |
| `COOKIE_SECURE` | `true`. When unset, an `https://` origin turns it on. The local `.env` value is `false`; do not copy that. | recommended |
| `ADMIN_EMAIL` | First Google sign-in of this address becomes the platform admin. | yes |
| `DEV_LOGIN` | `false`. Demo login is for Compose only. | yes |
| `SEED_DEMO` | `false`. | yes |
| `VAPID_PUBLIC_KEY` | Web Push public key | required for push |
| `VAPID_PRIVATE_KEY` | Web Push private key. Sensitive. | required for push |
| `VAPID_SUBJECT` | `mailto:noreply@pesenin.product.biz.id` (or your contact) | with the VAPID keys |
| `STORAGE_DRIVER` | `s3` when using R2. Default `local` keeps files in `/tmp/pesenin-uploads`, which disappears when the instance stops. | for durable proofs |
| `R2_ENDPOINT` | Cloudflare R2 S3 endpoint | with `STORAGE_DRIVER=s3` |
| `R2_BUCKET` | Bucket name | with `STORAGE_DRIVER=s3` |
| `R2_ACCESS_KEY_ID` | R2 access key. Sensitive. | with `STORAGE_DRIVER=s3` |
| `R2_SECRET_ACCESS_KEY` | R2 secret. Sensitive. | with `STORAGE_DRIVER=s3` |
| `SESSION_DAYS` | `30` | no |
| `CRON_SECRET` | Long random string. Sensitive. See the worker note below. | only if you schedule `/api/v1/internal/tick` |
| `API_INTERNAL_URL` | Leave unset. The service binding sets it. | no |
| `HTTP_ADDR` | Leave unset. Compose sets `:8080`. | no |
| `PORT` | Leave unset. Vercel sets it, and the API listens there. | no |
| `STORAGE_DIR` | Leave unset on Vercel. Compose uses `/data/uploads`. | no |
| `ORIGIN` | Leave unset. Compose sets it for the Node server. | no |

`VERCEL`, `VERCEL_URL`, `VERCEL_ENV`, and `VERCEL_PROJECT_PRODUCTION_URL` are platform variables. If `PUBLIC_ORIGIN` is empty on Vercel, the API falls back to `https://$VERCEL_URL`. That host changes every deployment, so Google will reject the callback until `PUBLIC_ORIGIN` is the stable dev origin.

### Google OAuth redirect URI

In Google Cloud Console → APIs & Services → Credentials → the **Web application** client, register the stable dev origin:

- Authorized JavaScript origin: `https://<dev-host>`
- Authorized redirect URI: `https://<dev-host>/api/v1/auth/google/callback`

`<dev-host>` is the hostname only, for example `pesenin-git-dev-your-team.vercel.app` (branch alias) or a custom domain attached to the dev/preview environment. The per-deployment host (`pesenin-<hash>-your-team.vercel.app`) changes on every push; registering that URL breaks sign-in on the next deploy.

Set `PUBLIC_ORIGIN` to `https://<dev-host>` and `GOOGLE_REDIRECT_URL` to the redirect URI above. The two must match.

## Worker

Compose runs a 30-second loop in the API process (open and close due batches, expire orders and top-ups, flush web push, reconcile wallets). The same loop starts inside the Go server on Vercel and keeps running while that instance is warm. After scale-to-zero, the next cold start runs one catch-up pass immediately, then resumes the 30-second loop.

For idle gaps, Pro plans can call the same pass on a schedule. Add this to `vercel.json` only on a plan that allows the interval (Hobby rejects anything more frequent than once a day, and the deploy fails):

```json
"schedules": {
  "worker-tick": {
    "schedule": "*/5 * * * *",
    "path": "/api/v1/internal/tick",
    "requestMethod": "GET"
  }
}
```

Set `CRON_SECRET`. Vercel sends `Authorization: Bearer <CRON_SECRET>`. The route answers 401 until that variable is set. Schedules on a custom environment start inactive; enable them once in that environment. Times of day in the product stay on the store timezone (WIB / WITA / WIT, default `Asia/Jakarta`). The schedule itself is UTC.

## Local checks

```bash
cd backend && go test ./internal/config ./internal/clock ./internal/httpapi ./internal/koin ./internal/orderflow ./internal/text ./internal/uniqueamount
cd backend && CGO_ENABLED=0 go build -o /tmp/pesenin-api ./cmd/api

cd frontend && npm ci && npm run build
cd frontend && VERCEL=1 npm run build
```

The first frontend build is the Compose adapter (`frontend/build`). The second is the Vercel adapter (`frontend/.vercel/output`).
