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

Behind a panel (aaPanel/BT) that manages TLS? Use `--no-nginx` and point the
panel's reverse proxy at `127.0.0.1:<port>`.

## Prerequisites

1. **Docker Engine + compose plugin** on the host.
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
