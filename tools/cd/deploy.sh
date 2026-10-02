#!/bin/sh
# CD por timer (opção A): a VPS é o único executor do deploy.
# O GitHub nunca alcança a VPS — a VPS puxa a origin/main e aplica.
set -eu

APP_DIR=${APP_DIR:-/opt/ndovu}
REMOTE=${CD_REMOTE:-origin}
BRANCH=${CD_BRANCH:-main}
LOCK=/run/lock/ndovu-cd.lock
CD_DIR="$APP_DIR/.cd"
LOG_DIR=/var/log/ndovu-cd
LOG="$CD_DIR/deploy.log"

# A unidade systemd redireciona stdout/stderr p/ /var/log/ndovu-cd/deploy.log.
# exec 2>&1 (stderr -> stdout) para uma única fonte de ordem no arquivo.
exec 2>&1

log() {
  printf '[%s] %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"
}

if [ -t 1 ]; then
  :
elif [ ! -d "$LOG_DIR" ]; then
  mkdir -p "$LOG_DIR"
fi

if ! mkdir "$LOCK" 2>/dev/null; then
  log "deploy já em andamento — nada a fazer"
  exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT INT TERM

cd "$APP_DIR"

if [ ! -f .env.production ]; then
  log "ERRO: .env.production não encontrado em $APP_DIR"
  exit 1
fi

compose() {
  docker compose --env-file .env.production \
    -f docker-compose.yml -f docker-compose.prod.yml "$@"
}

# --- estado de referência ----------------------------------------------------
mkdir -p "$CD_DIR"
if [ ! -f "$CD_DIR/deployed-sha" ]; then
  log "deploy por timer nunca marcou um SHA — assumindo a árvore atual"
  git rev-parse HEAD > "$CD_DIR/deployed-sha"
fi
deployed=$(cat "$CD_DIR/deployed-sha")

git fetch --quiet "$REMOTE"

# --- proteção de migration ---------------------------------------------------
# A app auto-aplica migrations no boot (idempotente), e elas são via-forward.
# Por isso a aplicação NUNCA é atualizada com um deploy automático quando há
# migration no caminho: o gate é a marca manual de release no GitHub
# (deploy-release). O CD avisa, marca o SHA pulado (não fica pedindo para
# sempre) e sai sem tocar no stack.
if git diff --name-only "$deployed".."$REMOTE/$BRANCH" -- \
    backend/internal/adapter/clickhouse/schema.sql \
    backend/internal/adapter/ctlpostgres 2>/dev/null | grep -q .; then
  target=$(git rev-parse "$REMOTE/$BRANCH")
  if [ "$(cat "$CD_DIR/skipped-sha" 2>/dev/null)" = "$target" ]; then
    log "migration pendente no $target — já avisado, esperando release manual"
    exit 0
  fi
  log "ATENÇÃO: migration entre $deployed e $target (schema.sql/ctlpostgres)."
  log "  O deploy automático NÃO aplica migrations: confirme o backup dos 2"
  log "  bancos e publique o release no GitHub (deploy-release) para aplicar."
  printf '%s\n' "$target" > "$CD_DIR/skipped-sha"
  exit 0
fi

# --- nada novo? ---------------------------------------------------------------
target=$(git rev-parse "$REMOTE/$BRANCH")
if [ "$target" = "$deployed" ]; then
  exit 0
fi

# --- working tree limpo (recusa misturar deploy com sujeira local) -----------
if ! git diff --quiet || ! git diff --cached --quiet; then
  log "ERRO: working tree sujo — commit/limpe antes do deploy por timer"
  exit 1
fi

log "deploy: $deployed -> $target"

# 1. composition valida sem subir nada (segredo fraco recusaria o boot)
compose config --quiet

# 2. aplica
git merge --ff-only "$REMOTE/$BRANCH"
compose build
compose up -d

# 3. smoke: /health por dentro da rede do compose. A imagem da api é
#    distroless (sem wget/curl/shell) — o probe roda de dentro do caddy
#    (Alpine, tem wget) contra api:8080 pela rede interna, a mesma rota
#    que o Caddyfile já usa pra fazer proxy de /api/*.
api_ready=no
i=1
while [ "$i" -le 30 ]; do
  if compose exec -T caddy wget -q -O- http://api:8080/health >/dev/null 2>&1; then
    api_ready=yes
    break
  fi
  sleep 2
  i=$((i + 1))
done

services=$(compose ps --format '{{.Name}} {{.Status}}' 2>/dev/null || compose ps)

if [ "$api_ready" != "yes" ]; then
  log "FALHA no smoke: /health não respondeu — NENHUMA mudança adicional."
  log "commit aplicado: $target (remanesce em produção). Reverte com:"
  log "  cd $APP_DIR && git checkout $deployed"
  log "  $(basename "$0" .sh) # re-executa build/up com o SHA anterior"
  log "serviços: $services"
  exit 1
fi

printf '%s\n' "$target" > "$CD_DIR/deployed-sha"
rm -f "$CD_DIR/skipped-sha"
log "deploy ok: $target (/health 200; serviços: $services)"
