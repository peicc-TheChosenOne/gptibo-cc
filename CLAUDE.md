# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

GPTibo：可自托管的商城前台、管理工作台（`/admin`）和代理工作台（`/partner`、`/join`）。这是**经过脱敏的开源版本**（MIT）：上游供应商和支付通道已匿名化（`recharge`、`account_supplier`、`delivery_supplier`、`gatewaya`、`gatewayb`、`xgift`），接入地址使用保留域名 `example.invalid`，真实支付、采购、邮件和机器人默认关闭。适配器仅作开发参考，不能直接用于生产。数据库结构和部分路由已匿名化，只支持新建的开发数据库。

**两套并行后端共用同一个 React 前端和同一套 `/api/*` 接口契约：**

1. **Go 后端（主实现）** — `services/storefront/`：Go + Gin + PostgreSQL。单一二进制，子命令为 `api | worker | migrate | grant-runtime | check-runtime | health-api | health-worker | import-d1-* | telegram-configure`（见 `cmd/storefront/main.go`）。业务逻辑在 `internal/<领域>` 包中（orders、payments、recharge、procurement、refunds、management、invoices、xgift 等）；HTTP 处理器在 `internal/httpapi`；后台任务在 `internal/worker`。PostgreSQL 迁移位于 `services/storefront/migrations/`（通过 `embed.go` 嵌入）。
2. **Cloudflare Worker + D1（备用实现）** — `worker/`：`worker/index.ts` 负责路由（回调 webhook、`/api/*` → `commerce.ts` 等，其余请求 → 静态资源），并包含 `scheduled` 定时任务入口。D1（SQLite）迁移位于根目录 `migrations/`。配置在 `wrangler.jsonc`（数据库 ID 为全零占位值，不绑定任何生产资源）。

修改接口时需同时保持两套后端与前端一致。Go 集成测试 `TestFrontendRouteContracts`（`services/storefront/integration/frontend_test.go`）会运行 `services/storefront/scripts/check-frontend-contracts.mjs`，用 TypeScript 编译器提取 `src/` 中所有 `/api/...` 调用，并与 `httpapi.RouteInventory()` 比对——因此需要先在根目录执行 `npm ci`。

其他顶层目录：
- `src/` — React 19 + Vite + Tailwind 4 前端。`src/main.tsx` / `Root.tsx` 按路径选择商城、管理端（`src/admin/`）或代理端（`src/partner/`）。`@` 是 `src/` 的别名。API 客户端在 `src/lib/api.ts`。
- `shared/` — 前端与 Worker 共用的纯 TS 逻辑（定价、订单状态、签名等）。
- `emails/` — React Email 邮件模板。`npm run emails:build`（`scripts/build-emails.tsx`）会将其渲染为**生成文件** `worker/emails.generated.ts` 和 `services/storefront/internal/mailer/templates.json`。只修改 `emails/*.tsx`，不要手改生成文件；`build` 和 `test` 会自动重新生成。

## 常用命令

需要 Node 22.18+（推荐 24；测试依赖原生 TS 类型剥离和 `node:sqlite`）、Python 3、Go 1.26+。

前端 / Worker（仓库根目录）：
```sh
npm ci
npm run dev            # Vite 运行在 127.0.0.1:5173，/api 代理到 8787 端口的 Worker
npm run build          # emails:build + tsc -b + worker tsc + vite build
npm test               # emails:build + node --test tests/*.test.ts
npm run lint           # oxlint
npm run security:scan  # scripts/scan-public-source.py
npm run preview:worker # build + wrangler dev（先执行 cp .dev.vars.example .dev.vars; npx wrangler d1 migrations apply gptibo-local --local）
npx vite --config services/storefront/vite.config.ts   # 前端运行在 5175，对接 8093 端口的 Go API
```

运行单个 TS 测试：`node --test tests/commerce.test.ts`（若改过邮件模板，先执行 `npm run emails:build`）。按名称过滤用 `--test-name-pattern="..."`。Worker 测试直接导入 `worker/index.ts`，并用 `migrations/*.sql` 构建的内存 `node:sqlite` 数据库模拟 D1。

Go 后端（`cd services/storefront`）：
```sh
go test ./...
go vet ./...
go test -run TestName ./internal/orders/
STOREFRONT_TEST_DATABASE_URL='postgres://user:pw@127.0.0.1:5432/storefront_test?sslmode=disable' go test -count=1 ./...
```
`services/storefront/integration/` 中的集成测试在未设置 `STOREFRONT_TEST_DATABASE_URL` 时会跳过；库名必须以 `_test` 结尾，且所有者角色需有 CREATE ROLE 权限（每个测试会创建随机 schema 和受限角色）。Docker Compose 启动方式及原生运行流程（先用所有者身份执行 `migrate` → `grant-runtime`，再用受限角色运行 `api` / `worker`；只读环境变量，不自动加载 dotenv）见 `services/storefront/README.md`。

CI（`.github/workflows/verify.yml`）执行：security:scan、lint、test、build、对已跟踪源码运行 Gitleaks，以及基于 PostgreSQL 18 的 Go vet / test。

## 安全与公开发布边界

- `scripts/scan-public-source.py` 会拒绝未知外联地址（白名单 `PUBLIC_HOSTS`）、未核验的二进制资源以及秘密/运行时文件。新增外部域名或二进制资源需有意识地更新白名单；占位地址使用 `example.invalid` / `example.test`。
- 不得提交 `.dev.vars`、`.env*`（示例文件除外）、`.secrets/`、数据库、日志、HAR、截图或密钥。前端环境变量不得存放服务器秘密。测试中的 RFC 4226/6238 测试向量不可用作真实管理员 TOTP 秘密。
- 支付、充值、采购代码中的回调、金额、签名、商户归属和幂等校验均为有意设计，必须保留。

## Git 工作流（来自 AGENTS.md）

- 每次完成代码改动及验证后及时提交，不能只部署不提交。提交前检查改动范围，运行 `git diff --check` 及相关验证；提交说明写清实际改动和验证结果。
- 不覆盖或丢弃其他任务的未提交改动；跨任务的历史整理仅在用户明确要求时执行。
- 重要发布使用带说明的版本标签记录部署版本；不为过去未保存的状态虚构提交或标签。
- 已配置远程时推送完成的分支和发布标签；未配置时说明只有本地记录，不自行创建远程仓库。
- 向用户报告提交、标签、验证结果和远程同步状态。
