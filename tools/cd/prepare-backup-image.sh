#!/bin/sh
# Garante o contexto `ndovu-clickhouse` (a imagem da stack) p/ o build da
# imagem de backup. Idempotente: nao re-baixa se a imagem ja existe.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
compose="docker compose --env-file $repo/.env.production -f $repo/docker-compose.yml -f $repo/docker-compose.prod.yml"
image=$($compose images clickhouse --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | head -1)
[ -n "$image" ] || image=clickhouse/clickhouse-server:24.8-alpine
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image"
ctx=/tmp/ndovu-clickhouse-ctx
rm -rf "$ctx"; mkdir -p "$ctx"
docker create --name ndovu-ctx-tmp "$image" >/dev/null 2>&1 || true
docker cp ndovu-ctx-tmp:/usr/bin/clickhouse "$ctx/clickhouse"
docker rm ndovu-ctx-tmp >/dev/null
echo "contexto pronto: $ctx (fonte $image)"
