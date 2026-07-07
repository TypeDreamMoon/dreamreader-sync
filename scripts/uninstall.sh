#!/bin/sh
# Tear down the dreamreader-sync deployment: stop the Docker stack and remove the
# host-Nginx site. The IAM platform stack is left alone. The SQLite data volume
# is KEPT by default (it holds every user's sync doc) — pass --purge to drop it.
#
# Usage:
#   sudo ./scripts/uninstall.sh --domain api.mr.64hz.cn
#   ./scripts/uninstall.sh --keep-images   # stop containers, keep built image
#   ./scripts/uninstall.sh --purge         # ALSO delete the data volume (destroys all synced data)
set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy/install"
ENV_FILE="$DEPLOY_DIR/.env"
DOMAIN=""
KEEP_IMAGES=n
PURGE=n

info() { printf '\033[1;36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$1" >&2; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$1" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)      DOMAIN="${2:?--domain needs a value}"; shift 2 ;;
    --keep-images) KEEP_IMAGES=y; shift ;;
    --purge)       PURGE=y; shift ;;
    -h|--help)     sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  die "docker compose not found"
fi
get_env() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -n1 | cut -d= -f2- ; }
compose() {
  _f="-f $DEPLOY_DIR/docker-compose.yml"
  [ "$(get_env DREAMSYNC_USE_IAM_NET)" = y ] && _f="$_f -f $DEPLOY_DIR/docker-compose.iam-network.yml"
  # shellcheck disable=SC2086
  ( cd "$DEPLOY_DIR" && $COMPOSE $_f --env-file "$ENV_FILE" "$@" )
}

[ -n "$DOMAIN" ] || DOMAIN="$(get_env DREAMSYNC_PUBLIC_DOMAIN)"

info "Stopping dreamreader-sync…"
# down flags: --rmi local removes the built image unless --keep-images; -v removes
# named volumes (the SQLite data) only on --purge.
_flags=""
[ "$KEEP_IMAGES" = y ] || _flags="$_flags --rmi local"
if [ "$PURGE" = y ]; then
  warn "--purge: the data volume (all synced user data) will be DELETED."
  _flags="$_flags -v"
fi
# shellcheck disable=SC2086
compose down $_flags

if [ -n "$DOMAIN" ]; then
  SITE_NAME="dreamreader-sync-$DOMAIN"
  for p in "/etc/nginx/sites-enabled/$SITE_NAME.conf" "/etc/nginx/sites-available/$SITE_NAME.conf"; do
    if [ -e "$p" ]; then
      [ "$(id -u)" = 0 ] || die "removing $p needs root; re-run with sudo"
      rm -f "$p"; info "removed $p"
    fi
  done
  if command -v nginx >/dev/null 2>&1 && [ "$(id -u)" = 0 ]; then
    nginx -t && { command -v systemctl >/dev/null 2>&1 && systemctl reload nginx || nginx -s reload; }
  fi
fi

info "Done.$([ "$PURGE" = y ] || printf ' (data volume kept — use --purge to delete it.)')"
