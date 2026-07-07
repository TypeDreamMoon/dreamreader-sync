#!/usr/bin/env bash
# dreamreader-sync deployment smoke test. Checks the health probe and that the
# sync endpoint is fail-closed (no token / bad token -> 401).
#
# Usage:
#   ./scripts/smoke.sh                          # defaults to http://127.0.0.1:8090
#   ./scripts/smoke.sh http://127.0.0.1:8090
#   ./scripts/smoke.sh https://api.mr.64hz.cn   # edge smoke (through Nginx/TLS)
set -u

BASE="${1:-http://127.0.0.1:8090}"
OK=0
FAIL=0

check() {
  # name, url, expected, [extra curl args...]
  local name="$1" url="$2" expected="$3"; shift 3
  local code
  code="$(curl --noproxy '*' -s -o /dev/null -w '%{http_code}' "$@" "$url" || echo 000)"
  if [ "$code" = "$expected" ]; then
    echo "  PASS: $name ($code)"; OK=$((OK + 1))
  else
    echo "  FAIL: $name (got $code, expected $expected)"; FAIL=$((FAIL + 1))
  fi
}

echo "== dreamreader-sync: $BASE =="
check "healthz"                 "$BASE/healthz"        200
check "GET sync without token"  "$BASE/api/v1/sync"    401                                   # fail-closed
check "GET sync with bad token" "$BASE/api/v1/sync"    401 -H "Authorization: Bearer garbage"
check "PUT sync without token"  "$BASE/api/v1/sync"    401 -X PUT -H "Content-Type: application/json" --data '{}'

echo ""
echo "RESULT: $OK passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
