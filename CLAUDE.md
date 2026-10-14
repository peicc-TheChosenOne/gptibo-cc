# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

GPTibo: a self-hostable storefront, admin workbench (`/admin`) and partner/agent workbench (`/partner`, `/join`). It is a **sanitized open-source release** (MIT): upstream suppliers and payment gateways are anonymized (`recharge`, `account_supplier`, `delivery_supplier`, `gatewaya`, `gatewayb`, `xgift`), endpoints use the reserved `example.invalid` domain, and real payments/procurement/mail/bots are off by default. Adapters are development references, not production-ready integrations. Schemas and some routes are anonymized, so only fresh dev databases are supported.

There are **two parallel backends serving the same React frontend and the same `/api/*` contract**:

1. **Go backend (primary)** — `services/storefront/`: Go + Gin + PostgreSQL. One binary, subcommands `api | worker | migrate | grant-runtime | check-runtime | health-api | health-worker | import-d1-* | telegram-configure` (`cmd/storefront/main.go`). Business logic lives in `internal/<domain>` packages (orders, payments, recharge, procurement, refunds, management, invoices, xgift, …); HTTP handlers in `internal/httpapi`; background jobs in `internal/worker`. Postgres migrations are in `services/storefront/migrations/` (embedded via `embed.go`).
2. **Cloudflare Worker + D1 (alternate)** — `worker/`: `worker/index.ts` routes requests (webhooks, `/api/*` → `commerce.ts` etc., everything else → static assets) and has a `scheduled` cron handler. D1 (SQLite) migrations are in root `migrations/`. Config in `wrangler.jsonc` (placeholder all-zero DB id, no production bindings).

When changing an API, keep both backends and the frontend consistent. The Go integration test `TestFrontendRouteContracts` (`services/storefront/integration/frontend_test.go`) runs `services/storefront/scripts/check-frontend-contracts.mjs`, which uses the TypeScript compiler to extract every `/api/...` call from `src/` and checks it against `httpapi.RouteInventory()` — so it requires root `npm ci`.

Other top-level pieces:
- `src/` — React 19 + Vite + Tailwind 4 frontend. `src/main.tsx`/`Root.tsx` pick storefront vs admin (`src/admin/`) vs partner (`src/partner/`) by pathname. `@` aliases `src/`. API client in `src/lib/api.ts`.
- `shared/` — pure TS logic shared by the frontend and the Worker (pricing, order status, signatures, etc.).
- `emails/` — React Email templates. `npm run emails:build` (`scripts/build-emails.tsx`) renders them into **generated** `worker/emails.generated.ts` and `services/storefront/internal/mailer/templates.json`. Edit `emails/*.tsx`, never the generated files; `build` and `test` regenerate them.

## Commands

Requires Node 22.18+ (24 recommended; tests rely on native TS type stripping and `node:sqlite`), Python 3, Go 1.26+.

Frontend / Worker (repo root):
```sh
npm ci
npm run dev            # Vite on 127.0.0.1:5173, proxies /api to Worker on 8787
npm run build          # emails:build + tsc -b + worker tsc + vite build
npm test               # emails:build + node --test tests/*.test.ts
npm run lint           # oxlint
npm run security:scan  # scripts/scan-public-source.py
npm run preview:worker # build + wrangler dev (after: cp .dev.vars.example .dev.vars; npx wrangler d1 migrations apply gptibo-local --local)
npx vite --config services/storefront/vite.config.ts   # frontend on 5175 against Go API on 8093
```

Single TS test: `node --test tests/commerce.test.ts` (run `npm run emails:build` first if templates changed). Filter by name with `--test-name-pattern="..."`. Worker tests import `worker/index.ts` directly and back D1 with an in-memory `node:sqlite` DB built from `migrations/*.sql`.

Go backend (`cd services/storefront`):
```sh
go test ./...
go vet ./...
go test -run TestName ./internal/orders/
STOREFRONT_TEST_DATABASE_URL='postgres://user:pw@127.0.0.1:5432/storefront_test?sslmode=disable' go test -count=1 ./...
```
Integration tests in `services/storefront/integration/` skip without `STOREFRONT_TEST_DATABASE_URL`; the DB name must end in `_test`, and the owner role needs CREATE ROLE (each test creates a random schema + restricted role). Docker Compose setup and native run (`migrate` → `grant-runtime` as owner, then `api`/`worker` as restricted role; env vars only, no dotenv loading) are documented in `services/storefront/README.md`.

CI (`.github/workflows/verify.yml`) runs: security:scan, lint, test, build, Gitleaks on tracked source, and Go vet/test against Postgres 18.

## Security / release boundary

- `scripts/scan-public-source.py` rejects unknown outbound hosts (allowlist `PUBLIC_HOSTS`), unapproved binary assets, and secret/runtime files. Adding a new external host or binary asset requires updating that allowlist deliberately; use `example.invalid` / `example.test` for placeholders.
- Never commit `.dev.vars`, `.env*` (except examples), `.secrets/`, databases, logs, HAR, screenshots, or keys. Frontend env vars must not hold server secrets. Don't reuse the RFC 4226/6238 test vectors in tests as real admin TOTP secrets.
- Callback, amount, signature, merchant-ownership and idempotency checks in payment/recharge/procurement code are intentional — preserve them.

## Git workflow (from AGENTS.md)

- After each code change and its verification, commit promptly (don't just deploy). Before committing, review the change scope and run `git diff --check` plus relevant verification; commit messages state the actual change and verification results.
- Don't overwrite or discard other tasks' uncommitted changes; do cross-task history cleanup only when explicitly asked.
- Tag important releases with annotated tags recording the deployed version; never fabricate commits/tags for past unsaved states.
- If a remote is configured, push finished branches and release tags; if not, say that only local history exists (don't create a remote).
- Report commits, tags, verification results and remote sync status to the user.
