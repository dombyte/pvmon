#!/bin/bash
# Initial frontend bundle budget (spec 14): entry JS + CSS < 250 KB gzip. Run after
# `npm run build` in frontend/; ECharts is loaded lazily and not part of the entry chunk.
set -eu
LIMIT=$((250 * 1024))
cd "$(dirname "$0")/../frontend/dist"
TOTAL=0
for f in assets/index-*.js assets/index-*.css; do
    [ -f "$f" ] || continue
    TOTAL=$((TOTAL + $(gzip -9 -c "$f" | wc -c)))
done
echo "initial bundle: ${TOTAL} bytes gzip (budget ${LIMIT})"
[ "$TOTAL" -le "$LIMIT" ]
