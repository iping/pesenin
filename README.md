# Pesenin

PO-first order tool for Indonesian home food sellers. Tagline: **Hungry? Ping me**. Mascot: Pingping.

Phase 1 (MVP v1) covers seller sign-in with Google or email and password, one brand and one store, pre-order batches, guest checkout, unique transfer amounts, proof upload, production recap, a koin wallet with manual top-up, and a super-admin queue.

Sellers can also verify a WhatsApp number. Paid orders and koin top-up stay closed until the owner's email and WhatsApp are verified (`KYC_GATE=contact`). Office Pesenin can then approve, reject, or suspend a brand. Admin routes still require a Google session (or the local demo login), not a password session.

Public pages, the seller app (`/app`), and the API (`/api`) share one origin.

## Run locally

```bash
cp .env.example .env
docker compose up --build
```

Open http://localhost:43123.

Demo sign-in is on in Compose (`DEV_LOGIN=true`):

- Seller: `rini@dapur.test` (shop **Dapur Senja**, 5.000 koin)
- Super admin: `admin@pesenin.test`

Turn `DEV_LOGIN` off outside local development.

## Environment

| Variable | Purpose |
| --- | --- |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Seller Google sign-in. Callback: `{PUBLIC_ORIGIN}/api/v1/auth/google/callback` |
| `MAIL_DRIVER`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | Transactional email. Empty `SMTP_HOST` (or `MAIL_DRIVER=log`) prints confirmation and reset links in the API log. |
| `WA_PROVIDER`, `WA_API_URL`, `WA_API_TOKEN`, `WA_SENDER_ID` | WhatsApp OTP. `log` prints the code. `fonnte`, `wablas`, and `meta` are reserved and not implemented yet. |
| `KYC_GATE` | `contact` (default) blocks paid orders and koin top-up until email and WhatsApp are verified. `off` disables the gate. |
| `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT` | Web Push. If empty, the API writes a dev key pair into the upload volume. |
| `R2_ENDPOINT`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | Cloudflare R2 for transfer proofs. Leave empty to store files on local disk. |
| `ADMIN_EMAIL` | First sign-in of this address is the platform admin. |
| `DEV_LOGIN`, `SEED_DEMO` | Local demo login and the Dapur Senja seed. |

The launch admin fee is still undecided (Rp 0 to Rp 2.500). The database ships a versioned default of **Rp 1.000**, which the super admin can replace without a deploy.

## Tests

```bash
docker compose up -d db
cd backend
TEST_DATABASE_URL='postgres://pesenin:pesenin@127.0.0.1:54329/pesenin_test?sslmode=disable' go test ./...
```

Covered: unique-amount allocation, the koin ledger, the order state machine, and top-up approval.

Image encode, EXIF orientation, and variant sizes do not need Postgres:

```bash
cd backend && go test ./internal/images/
```

Budgets and measured file sizes are in [docs/images.md](docs/images.md).

## Single VPS

One machine can run the Compose stack behind Caddy on `pesenin.product.biz.id`.

1. Point the DNS A record at the VPS.
2. Set `PUBLIC_ORIGIN=https://pesenin.product.biz.id` and `COOKIE_SECURE=true`.
3. Set `DEV_LOGIN=false` and `SEED_DEMO=false`.
4. Put the Google, VAPID, and R2 values in `.env`.
5. Terminate TLS in Caddy (`reverse_proxy` to the Compose proxy, or replace the bundled Caddyfile with a host Caddy that routes `/api` to the API and everything else to the web service).
6. Keep the Postgres volume and the upload volume. Proofs stay 90 days; back up the database daily (`pg_dump`).

The API binary runs migrations on startup.

## Vercel (DEV)

One project can serve the SvelteKit app and the Go API on the same origin. Services and the Go runtime are in beta. Compose is unchanged. Settings, the env var list, the Google redirect URI, and the Neon migration command are in [DEPLOY-VERCEL.md](DEPLOY-VERCEL.md).
