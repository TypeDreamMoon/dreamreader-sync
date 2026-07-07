# Deploying dreamreader-sync

A single Go + SQLite container that stores one sync document per user for the
Dream Manga Reader app. Identity comes from the IAM platform — this service only
validates IAM access tokens (JWKS) and never runs its own IdP.

## One-liner (interactive)

From the repo root, on the target host:

```sh
sudo ./scripts/install.sh
```

The wizard asks for the public domain, the IAM URL, the `client_id`, the port,
and how to fetch JWKS, writes `deploy/install/.env`, then builds + starts the
container and renders a host-Nginx site. Enable HTTPS with
`certbot --nginx -d <domain>` afterwards.

Behind a panel (aaPanel/BT) that manages TLS? Use `--no-nginx` (see below).

## 宝塔面板 / aaPanel 反向代理

用宝塔时,装服务时跳过脚本自带的 Nginx,由宝塔做反代 + TLS:

```sh
sudo ./scripts/install.sh --no-nginx
```

服务此时监听 `127.0.0.1:8090`(端口以你在向导里填的为准)。然后在宝塔里:

1. **网站 → 添加站点**:域名填 `api.mr.64hz.cn`,类型选纯静态(不要 PHP、不建数据库)。
2. 该站点 **→ 设置 → 反向代理 → 添加反向代理**:
   - 目标 URL:`http://127.0.0.1:8090`
   - 发送域名(Host):`$host`
3. 该站点 **→ 设置 → 配置文件**:把请求体上限提到 **12m**(同步文档上限 8 MiB,留富余),
   否则大一点的同步会被 `413` 拒掉。在 `server { … }` 里加一行:
   ```nginx
   client_max_body_size 12m;
   ```
   宝塔反代默认已带 `Host / X-Real-IP / X-Forwarded-*` 头,一般无需再加。
4. 该站点 **→ SSL**:用 Let's Encrypt 申请证书并开「强制 HTTPS」。

验证(通过宝塔的公网入口):

```sh
./scripts/smoke.sh https://api.mr.64hz.cn
```

> ⚠️ IAM 里 `dream_manga_reader` 的 issuer/audience 与 App 端填的「IAM 地址」要用
> **HTTPS 公网域名**(`https://account.64hz.cn`),别用 `127.0.0.1`——令牌签发方
> (issuer)必须和校验方一致。

## Prerequisites

1. **Docker Engine + compose plugin** on the host. The build is **self-contained**
   (the IAM validator is vendored under `internal/authmw`) — clone this repo
   anywhere, under any directory name, with no sibling repos needed.
2. **The IAM platform is deployed** and reachable at the issuer URL.
3. **The `dream_manga_reader` client is registered in IAM** as a **public
   client** with **`authorization_code` + `refresh_token`** grants and **PKCE**,
   and the app's redirect URIs allow-listed:
   - Android: `dreammangareader://auth`
   - Windows/Linux: `http://localhost:8765/`

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | the service, driven by `.env` |
| `docker-compose.iam-network.yml` | overlay: join the IAM Docker network for internal JWKS |
| `.env.example` | annotated config template (`install.sh` generates `.env`) |
| `nginx-site.conf.tmpl` | host-Nginx site, rendered with the domain + port |

## Common commands

```sh
sudo ./scripts/install.sh            # first install (interactive)
sudo ./scripts/install.sh --setup    # re-run the wizard
./scripts/install.sh --update        # git pull + rebuild + recreate
./scripts/smoke.sh                    # health + auth-gate smoke test
sudo ./scripts/uninstall.sh          # stop + remove site (keeps data volume)
sudo ./scripts/uninstall.sh --purge  # ALSO delete the data volume
```

## Backup

All data is the SQLite file in the `dreamsync_data` volume
(`/data/dreamsync.db`). Back it up by copying that file (or the volume). Losing
it loses every user's synced blob — but clients re-push on next sync.
