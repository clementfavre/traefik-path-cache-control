#!/usr/bin/env bash
# End-to-end test: a real Traefik loads the plugin as a local plugin, in front of nginx
set -euo pipefail

cd "$(dirname "$0")"

URL="http://localhost:18080"
HOST="Host: site.localhost"
LONG="public, max-age=2592000"
BIG_SIZE=5000000
failures=0

cleanup() {
  docker compose down --remove-orphans >/dev/null 2>&1 || true
  rm -f site/media/big.bin
}
trap cleanup EXIT

# Large file to check that responses are streamed, not buffered
head -c "$BIG_SIZE" /dev/urandom > site/media/big.bin

docker compose up -d --quiet-pull

# Wait until Traefik routes to the backend
for _ in $(seq 1 30); do
  if curl -fs -o /dev/null -H "$HOST" "$URL/index.html"; then
    break
  fi
  sleep 1
done

# Usage: check <name> <expected status> <expected Cache-Control, "-" if none> <curl args...>
check() {
  local name=$1 want_status=$2 want_cache=$3
  shift 3

  local headers status cache
  headers=$(curl -s -o /dev/null -D - -H "$HOST" "$@" | tr -d '\r')
  status=$(echo "$headers" | awk 'NR == 1 { print $2 }')
  cache=$(echo "$headers" | grep -i '^cache-control:' | cut -d' ' -f2- || true)
  cache=${cache:--}

  if [ "$status" = "$want_status" ] && [ "$cache" = "$want_cache" ]; then
    echo "ok    $name"
  else
    echo "FAIL  $name: got $status '$cache', want $want_status '$want_cache'"
    failures=$((failures + 1))
  fi
}

check "css" 200 "$LONG" "$URL/css/main.css"
check "js" 200 "$LONG" "$URL/js/main.js"
check "media" 200 "$LONG" "$URL/media/photo.jpg"
check "head" 200 "$LONG" -I "$URL/css/main.css"
check "partial content" 206 "$LONG" -r 0-99 "$URL/media/big.bin"

etag=$(curl -s -o /dev/null -D - -H "$HOST" "$URL/css/main.css" | tr -d '\r' | grep -i '^etag:' | cut -d' ' -f2)
check "not modified" 304 "$LONG" -H "If-None-Match: $etag" "$URL/css/main.css"

check "page untouched" 200 "no-cache" "$URL/index.html"
check "lookalike untouched" 200 "no-cache" "$URL/cssfoo/main.css"
check "post untouched" 200 "no-cache" -X POST "$URL/media/upload"
check "404 untouched" 404 "-" "$URL/media/missing.jpg"
check "redirect untouched" 302 "no-cache" "$URL/media/old.jpg"

# Large file: full size and Content-Length kept
length=$(curl -s -o /dev/null -D - -H "$HOST" "$URL/media/big.bin" | tr -d '\r' | grep -i '^content-length:' | cut -d' ' -f2)
received=$(curl -s -H "$HOST" "$URL/media/big.bin" | wc -c | tr -d ' ')
if [ "$length" = "$BIG_SIZE" ] && [ "$received" = "$BIG_SIZE" ]; then
  echo "ok    large file streamed with Content-Length"
else
  echo "FAIL  large file: Content-Length '$length', received $received bytes, want $BIG_SIZE"
  failures=$((failures + 1))
fi

logs=$(docker compose logs traefik 2>&1)
if echo "$logs" | grep -q 'Plugins loaded'; then
  echo "ok    plugin loaded"
else
  echo "FAIL  plugin not loaded"
  failures=$((failures + 1))
fi

if echo "$logs" | grep -q ' ERR '; then
  echo "FAIL  errors in Traefik logs:"
  echo "$logs" | grep ' ERR '
  failures=$((failures + 1))
fi

if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed"
  exit 1
fi

echo "All checks passed"
