# GPTibo Go 后端

Go + Gin + PostgreSQL，API 与业务 Worker 使用同一个二进制。此版本仅面向全新安装；匿名化后的迁移与原部署不兼容，不导入生产快照。

## Docker 本地启动

需要 Docker Compose，命令在本目录执行。首先生成三个独立随机秘密（Python 3）：

```sh
python3 scripts/init-secrets.py
cp .env.example .env
docker compose config --quiet
docker compose build api
docker compose up -d --wait postgres
docker compose --profile maintenance run --rm maintenance migrate
docker compose --profile maintenance run --rm maintenance grant-runtime
docker compose up -d --wait api worker
```

也可以在 PowerShell 使用 `./scripts/init-secrets.ps1` 初始化。秘密文件位于被忽略的 `.secrets/`，已有文件不会覆盖。API 仅监听宿主机 `127.0.0.1:8093`，PostgreSQL 不映射宿主机端口；运行数据库角色与迁移所有者分离。

`compose.auth.yaml` 可选启用管理员/邮件：配置 `ADMIN_USERNAME`、`ADMIN_EMAIL`、`MAIL_FROM`，自行生成 `.secrets/admin-totp` 并保存 `.secrets/resend-key`，再叠加该 Compose 文件。不要使用测试中的固定认证向量。

其他接入参数见 `.env.integrations.example`。复制为 `.env.integrations` 后可叠加 `compose.integrations.yaml`；所有开关默认关闭，匿名适配器的保留域名无法用于真实交易。必须先按自己的上游协议完成开发和测试，不能直接填生产秘密试运行。可选域名配置在 `compose.domains.yaml`，仅使用自己新建的云资源。

## 服务器子域名入口（Nginx + HTTPS）

Go API 和 Worker 可以保持 Docker Compose 部署，由服务器 Nginx 提供前端文件并转发 API。公网使用域名后，不再需要客户端 SSH 隧道。

本目录 `deploy/nginx-public.conf.example` 是独立虚拟主机示例。将 `store.example.invalid` 替换为自己的子域名，并按实际部署调整目录和前端版本路径；不要覆盖其他站点或通配域名规则。示例中的运行目录是 `/opt/gptibo-cc/preview`，Nginx 主配置路径适用于宝塔环境。

1. 将子域名解析到服务器。API 继续只绑定 `127.0.0.1:8093`，数据库不开放宿主机端口；公网入口使用 Nginx 的 80/443。
2. 首次签发证书前，仅加载示例的 HTTP server 块，并暂时将其 `location /` 改为 `return 503`。创建 `acme-webroot/.well-known/acme-challenge`，运行 `nginx -t` 后重载，验证公网能读取该目录的测试文件。
3. 在独立 Python 虚拟环境 `certbot-venv` 安装 Certbot。使用下面的 webroot 命令签发证书，目录均独立于已有站点：

   ```sh
   APP_ROOT=/opt/gptibo-cc/preview
   DOMAIN=store.example.invalid # 改成已解析到自己的服务器的域名
   "$APP_ROOT/certbot-venv/bin/certbot" certonly --webroot \
     --webroot-path "$APP_ROOT/acme-webroot" --domain "$DOMAIN" --cert-name "$DOMAIN" \
     --config-dir "$APP_ROOT/letsencrypt" --work-dir "$APP_ROOT/acme-work" \
     --logs-dir "$APP_ROOT/logs/letsencrypt" \
     --agree-tos --non-interactive --register-unsafely-without-email --key-type ecdsa
   ```

4. 在实际运行的 Compose 环境中将 API 和 Worker 的 `PUBLIC_ORIGIN` 改为精确的 `https://子域名`（不含路径和末尾斜线）。查看 API 容器网络中的宿主机网关，只将该入口地址以 `/32` 加入 `TRUSTED_PROXY_CIDRS`。Nginx 会覆盖转发头；不要信任整个私网或客户端传入的地址。检查 Compose 配置后，仅更新本项目的 API 和 Worker。
5. 安装完整的 Nginx 示例，检查配置后平滑重载。验证 HTTP 跳转、HTTPS 证书、首页、`/admin`、`/api/catalog`、`/healthz` 及一次演示订单；同时复查已有站点。
6. 将 `reload-nginx.sh.example` 安装为 `$APP_ROOT/reload-public-nginx.sh`（root 所有、750 权限）。将两个 Certbot systemd 示例分别安装为 `gptibo-preview-certbot.service` 和 `gptibo-preview-certbot.timer`，执行 `systemd-analyze verify`、`systemctl daemon-reload`、`systemctl enable --now gptibo-preview-certbot.timer`。用相同目录参数运行 `certbot renew --dry-run --no-random-sleep-on-renew` 验证续期。将 `logrotate-public.example` 安装到 `/etc/logrotate.d/gptibo-preview-public`，并用 `logrotate --debug` 检查；如服务器没有 `www` 用户，需修改日志创建用户。

变更前备份实际部署配置，保留原有数据库和卷。证书账户、私钥和管理员验证器密钥仅保存在服务器私有目录，不提交到 Git。源码中的这些文件是脱敏部署示例，不包含实际凭据。公开访问不会自动启用邮件或真实支付；邮件关闭时，现有管理员登录仍无法完成邮箱验证码验证。

## 原生开发

程序读取环境变量，不自动加载 dotenv。通过 `DATABASE_URL_FILE` 或 `DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD_FILE` 指向独立数据库；外部连接默认要求 TLS。先用所有者身份执行 `migrate` 和 `grant-runtime`，再用受限角色执行 `api` 和 `worker`：

```sh
go run ./cmd/storefront migrate
go run ./cmd/storefront grant-runtime
go run ./cmd/storefront api
# 另一个终端使用相同运行配置：
go run ./cmd/storefront worker
```

`grant-runtime` 还需 `APP_DB_PASSWORD_FILE`。运行身份需 `AUTH_SECRET_FILE`，默认公开来源为 `http://127.0.0.1:8093`。配置管理员与邮件后才能使用完整后台登录。根目录执行 `npx vite --config services/storefront/vite.config.ts` 启动前端。

## 测试

先在仓库根目录执行 `npm ci`；前后端路由契约测试需要 TypeScript 解析器。

```sh
go test ./...
go vet ./...
# 专用本机 PostgreSQL，库名必须以 _test 结尾：
STOREFRONT_TEST_DATABASE_URL='postgres://test_user:YOUR_LOCAL_PASSWORD@127.0.0.1:5432/storefront_test?sslmode=disable' go test -count=1 ./...
```

每个集成测试建立随机 schema 和受限角色，并在结束后清理，需要测试数据库所有者拥有创建角色权限。不可指向业务数据库；没有连接配置时，集成测试明确跳过。
