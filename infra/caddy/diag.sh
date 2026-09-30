#!/bin/sh
# Diagnóstico do Caddy sob SNI estranho: roda a config real, loga em stdout.
set -x
caddy version
caddy validate --adapter caddyfile --config /etc/caddy/Caddyfile 2>&1 | grep -v '^{"' | tail -1
caddy run --adapter caddyfile --config /etc/caddy/Caddyfile &
P=$!
sleep 6
echo "== teste 127 =="
curl -sk -o /dev/null -w "code=%{http_code}\n" https://127.0.0.1/ 2>&1
curl -skv https://127.0.0.1/ 2>&1 | grep -E "subject:|issuer:|HTTP/|alert|SSL" | head -6
echo "== admin: servers =="
wget -qO- http://localhost:2019/config/apps/http/servers/srv0 2>/dev/null | head -c 400
echo ""
echo "== admin: tls =="
wget -qO- http://localhost:2019/tls/certificates/automatic 2>/dev/null | head -c 300
echo ""
kill $P 2>/dev/null
