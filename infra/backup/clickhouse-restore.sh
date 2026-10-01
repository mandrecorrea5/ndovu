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

backup_path=${1:?usage: clickhouse-restore /clickhouse/YYYY/MM/DD/ndovu-ch-TIMESTAMP/}
backup_path=${backup_path#/}

case "$NDOVU_CH_BACKUP_S3_ENDPOINT" in
  https://*) ;;
  *)
    echo "NDOVU_CH_BACKUP_S3_ENDPOINT must use HTTPS" >&2
    exit 2
    ;;
esac

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

# Argumentos de S3(...) sao literais entre aspas. 3 args: na 24.8 o 4o vira
# region invalida (Code:42).
restore_uri="S3($(sql_str "$endpoint/$backup_path"), $(sql_str "$NDOVU_CH_BACKUP_S3_ACCESS_KEY"), $(sql_str "$NDOVU_CH_BACKUP_S3_SECRET_KEY"))"

# ASYNC devolve "id<TAB>status"; so o id interessa.
restore_id=$(query "RESTORE DATABASE \`$NDOVU_CH_DATABASE\` FROM $restore_uri ASYNC" | cut -f1)
printf 'restore %s: started\n' "$restore_id"

status=
while :; do
  status=$(query "SELECT status FROM system.backups WHERE id = $(sql_str "$restore_id")")
  case "$status" in
    RESTORED|RESTORE_FAILED) break ;;
    '')
      printf 'restore %s not found in system.backups\n' "$restore_id" >&2
      exit 1
      ;;
  esac
  sleep 5
done

if [ "$status" != "RESTORED" ]; then
  error=$(query "SELECT leftUTF8(error, 500) FROM system.backups WHERE id = $(sql_str "$restore_id")")
  printf 'restore %s failed: %s\n' "$restore_id" "$error" >&2
  exit 1
fi

printf 'ClickHouse restore completed into database %s from %s\n' \
  "$NDOVU_CH_DATABASE" "$backup_path"
