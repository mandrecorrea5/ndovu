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

publish_deploy_status() {
  status_sha=$1
  status_state=$2
  status_description=$3

  status_token=$(awk -F= '
    $1 == "NDOVU_GITHUB_STATUS_TOKEN" {
      sub(/^[^=]*=/, "")
      sub(/\r$/, "")
      print
      exit
    }
  ' .env.production)
  if [ -z "$status_token" ]; then
    log "ERRO: NDOVU_GITHUB_STATUS_TOKEN ausente em .env.production"
    return 1
  fi

  remote_url=$(git remote get-url "$REMOTE")
  case "$remote_url" in
    git@github.com:*) repository=${remote_url#git@github.com:} ;;
    https://github.com/*) repository=${remote_url#https://github.com/} ;;
    *)
      log "ERRO: remoto $REMOTE não aponta para github.com; não é possível publicar status"
      return 1
      ;;
  esac
  repository=${repository%.git}
  case "$repository" in
    */*) ;;
    *)
      log "ERRO: URL do remoto GitHub inválida"
      return 1
      ;;
  esac

  if ! curl --config - <<EOF
fail
silent
show-error
connect-timeout = 10
max-time = 30
request = "POST"
header = "Accept: application/vnd.github+json"
header = "Authorization: Bearer $status_token"
header = "X-GitHub-Api-Version: 2022-11-28"
url = "https://api.github.com/repos/$repository/statuses/$status_sha"
data = "{\"state\":\"$status_state\",\"context\":\"ndovu/deploy\",\"description\":\"$status_description\"}"
output = "/dev/null"
EOF
  then
    log "ERRO: GitHub não aceitou o status $status_state para $status_sha"
    return 1
  fi
  log "status GitHub '$status_state' publicado para $status_sha"
}

# --- estado de referência ----------------------------------------------------
mkdir -p "$CD_DIR"
if [ ! -f "$CD_DIR/deployed-sha" ]; then
  log "deploy por timer nunca marcou um SHA — assumindo a árvore atual"
  git rev-parse HEAD > "$CD_DIR/deployed-sha"
fi
deployed=$(cat "$CD_DIR/deployed-sha")

git fetch --quiet --tags "$REMOTE"
target=$(git rev-parse "$REMOTE/$BRANCH")

# --- proteção de migration ---------------------------------------------------
# A app auto-aplica migrations no boot (idempotente), e elas são via-forward.
# Por isso a aplicação NUNCA é atualizada com um deploy automático quando há
# migration no caminho sem aprovação manual no GitHub. O workflow cria uma
# tag imutável para o SHA aprovado; a VPS mantém o pull-only e executa backups.
approved=no
if git diff --name-only "$deployed".."$REMOTE/$BRANCH" -- \
    backend/internal/adapter/clickhouse/schema.sql \
    backend/internal/adapter/ctlpostgres 2>/dev/null | grep -q .; then
  approval_tag="deploy-approved-$target"
  if git show-ref --verify --quiet "refs/tags/$approval_tag"; then
    approved_sha=$(git rev-parse "$approval_tag^{commit}")
    if [ "$approved_sha" != "$target" ]; then
      log "ERRO: tag $approval_tag aponta para $approved_sha em vez de $target"
      exit 1
    fi
    approved=yes
  fi
  if [ "$approved" != "yes" ]; then
    if [ "$(cat "$CD_DIR/skipped-sha" 2>/dev/null)" != "$target" ]; then
      log "ATENÇÃO: migration entre $deployed e $target."
      log "  Aguardando aprovação manual pelo workflow 'Approve migration deploy'."
      printf '%s\n' "$target" > "$CD_DIR/skipped-sha"
    else
      log "migration pendente no $target — aguardando aprovação manual no GitHub"
    fi
    exit 0
  fi
fi

# --- nada novo? ---------------------------------------------------------------
if [ "$target" = "$deployed" ]; then
  approval_tag="deploy-approved-$target"
  if git show-ref --verify --quiet "refs/tags/$approval_tag"; then
    approved_sha=$(git rev-parse "$approval_tag^{commit}")
    if [ "$approved_sha" != "$target" ]; then
      log "ERRO: tag $approval_tag aponta para $approved_sha em vez de $target"
      exit 1
    fi
    if compose exec -T caddy wget -q -O- http://api:8080/health >/dev/null 2>&1; then
      publish_deploy_status "$target" success "Deploy aplicado; health check passou"
    else
      log "ERRO: SHA aprovado consta como aplicado, mas o health check falhou"
      exit 1
    fi
  fi
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

# 2. Releases com migration só são aplicados após backup confirmado dos dois
# bancos. A chave de cifra é necessária para que a API inicie após a migration.
if [ "$approved" = "yes" ]; then
  if ! awk -F= '
    /^NDOVU_API_KEY_ENC_KEY=/ {
      value = substr($0, index($0, "=") + 1)
    }
    END {
      if (length(value) >= 32 && value !~ /[[:space:]#]/ && value !~ /^CHANGE_ME/) exit 0
      exit 1
    }
  ' .env.production; then
    log "ERRO: NDOVU_API_KEY_ENC_KEY ausente ou inválida em .env.production"
    exit 1
  fi
  publish_deploy_status "$target" pending "Aprovado; aguardando backups e health check"
  log "migration aprovada; executando backup do PostgreSQL"
  compose --profile backup run --build --rm postgres-backup
  log "backup do PostgreSQL confirmado; executando backup do ClickHouse"
  compose --profile backup run --build --rm clickhouse-backup
  log "backups dos dois bancos confirmados"
fi

# 3. aplica
git merge --ff-only "$REMOTE/$BRANCH"
compose build
compose up -d

# 2.5 O Caddyfile é bind mount: `compose up -d` não recria o container só
#     porque o conteúdo do arquivo mudou (o compose compara a definição do
#     serviço, não o arquivo montado), então o Caddy em execução continua
#     com a config antiga carregada até alguém mandar recarregar. Reload é
#     idempotente — rodar todo deploy, mesmo sem mudança no Caddyfile, não
#     tem custo.
caddy_reloaded=no
i=1
while [ "$i" -le 5 ]; do
  if compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; then
    caddy_reloaded=yes
    break
  fi
  sleep 2
  i=$((i + 1))
done

if [ "$caddy_reloaded" != "yes" ]; then
  log "FALHA: caddy reload não confirmou — a config em execução pode estar"
  log "  desatualizada mesmo com o deploy aplicado. commit: $target"
  exit 1
fi

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
if [ "$approved" = "yes" ]; then
  publish_deploy_status "$target" success "Deploy aplicado; health check passou"
fi
