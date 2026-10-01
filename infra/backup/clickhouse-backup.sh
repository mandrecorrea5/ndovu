#!/bin/sh
set -eu
umask 077

: "${NDOVU_CH_HOST:?NDOVU_CH_HOST is required}"
: "${NDOVU_CH_PORT:?NDOVU_CH_PORT is required}"
: "${NDOVU_CH_USER:?NDOVU_CH_USER is required}"
: "${NDOVU_CH_PASSWORD:?NDOVU_CH_PASSWORD is required}"
: "${NDOVU_CH_DATABASE:?NDOVU_CH_DATABASE is required}"
: "${NDOVU_CH_BACKUP_S3_ENDPOINT:?NDOVU_CH_BACKUP_S3_ENDPOINT is required}"
: "${NDOVU_CH_BACKUP_S3_ACCESS_KEY:?NDOVU_CH_BACKUP_S3_ACCESS_KEY is required}"
: "${NDOVU_CH_BACKUP_S3_SECRET_KEY:?NDOVU_CH_BACKUP_S3_SECRET_KEY is required}"
: "${NDOVU_CH_BACKUP_BUCKET:?NDOVU_CH_BACKUP_BUCKET is required}"

case "$NDOVU_CH_BACKUP_S3_ENDPOINT" in
  https://*) ;;
  *)
    echo "NDOVU_CH_BACKUP_S3_ENDPOINT must use HTTPS" >&2
    exit 2
    ;;
esac

# R2 manda o endpoint com o bucket no caminho; tira a / final para nao gerar //.
endpoint=${NDOVU_CH_BACKUP_S3_ENDPOINT%/}

query() {
  clickhouse-client \
    --host "$NDOVU_CH_HOST" \
    --port "$NDOVU_CH_PORT" \
    --user "$NDOVU_CH_USER" \
    --password "$NDOVU_CH_PASSWORD" \
    --query "$1"
}

# Literal de string SQL: escapa \ e ' e envolve em aspas simples.
sql_str() {
  printf "'%s'" "$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e "s/'/\\\\'/g")"
}

query "SELECT count() FROM system.tables WHERE database = $(sql_str "$NDOVU_CH_DATABASE")" >/dev/null

date_part=$(date -u '+%Y/%m/%d')
timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
prefix="clickhouse/${date_part}/ndovu-ch-${timestamp}"

access_key=$(sql_str "$NDOVU_CH_BACKUP_S3_ACCESS_KEY")
secret_key=$(sql_str "$NDOVU_CH_BACKUP_S3_SECRET_KEY")

# Argumentos de S3(...) sao literais entre aspas. 3 args: na 24.8 o 4o vira
# region invalida (Code:42).
backup_uri="S3($(sql_str "$endpoint/$prefix/"), $access_key, $secret_key)"

# ASYNC devolve "id<TAB>status"; so o id interessa.
backup_id=$(query "BACKUP DATABASE \`$NDOVU_CH_DATABASE\` TO $backup_uri ASYNC" | cut -f1)
printf 'backup %s: started\n' "$backup_id"

status=
while :; do
  status=$(query "SELECT status FROM system.backups WHERE id = $(sql_str "$backup_id")")
  case "$status" in
    BACKUP_CREATED|BACKUP_FAILED) break ;;
    '')
      printf 'backup %s not found in system.backups\n' "$backup_id" >&2
      exit 1
      ;;
  esac
  sleep 5
done

if [ "$status" != "BACKUP_CREATED" ]; then
  error=$(query "SELECT leftUTF8(error, 500) FROM system.backups WHERE id = $(sql_str "$backup_id")")
  printf 'backup %s failed: %s\n' "$backup_id" "$error" >&2
  exit 1
fi

# Formato One: 1 linha por objeto sem ler o conteudo.
files=$(query "SELECT count() FROM s3($(sql_str "$endpoint/$prefix/**"), $access_key, $secret_key, 'One')")

if [ "$files" -eq 0 ]; then
  echo "backup finished but no objects were written to the destination" >&2
  exit 1
fi

printf 'ClickHouse backup completed: %s (%s objects)\n' "$prefix" "$files"
