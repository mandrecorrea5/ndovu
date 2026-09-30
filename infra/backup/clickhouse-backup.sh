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

clickhouse-client \
  --host "$NDOVU_CH_HOST" \
  --port "$NDOVU_CH_PORT" \
  --user "$NDOVU_CH_USER" \
  --password "$NDOVU_CH_PASSWORD" \
  --query "SELECT count() FROM system.tables WHERE database = '$NDOVU_CH_DATABASE'" >/dev/null

date_part=$(date -u '+%Y/%m/%d')
timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
prefix="clickhouse/${date_part}/ndovu-ch-${timestamp}"

# 3 args em TO S3: 24.8 trata o 4º como region inválida (Code:42).
# Endpoint R2 já inclui o caminho do bucket; o prefixo do backup vem depois.
backup_uri="s3($NDOVU_CH_BACKUP_S3_ENDPOINT/$prefix/, $NDOVU_CH_BACKUP_S3_ACCESS_KEY, $NDOVU_CH_BACKUP_S3_SECRET_KEY)"

query() {
  clickhouse-client \
    --host "$NDOVU_CH_HOST" \
    --port "$NDOVU_CH_PORT" \
    --user "$NDOVU_CH_USER" \
    --password "$NDOVU_CH_PASSWORD" \
    --query "$1"
}

backup_id=$(query "BACKUP DATABASE $NDOVU_CH_DATABASE TO $backup_uri ASYNC")
printf 'backup %s: started\n' "$backup_id"

status=
while :; do
  status=$(query "SELECT status FROM system.backups WHERE id = '$backup_id'")
  case "$status" in
    BACKUP_CREATED|BACKUP_FAILED) break ;;
  esac
  sleep 5
done

if [ "$status" != "BACKUP_CREATED" ]; then
  error=$(query "SELECT leftUTF8(error, 500) FROM system.backups WHERE id = '$backup_id'")
  printf 'backup %s failed: %s\n' "$backup_id" "$error" >&2
  exit 1
fi

files=$(query "SELECT count() FROM system.backup_storage('s3', '$NDOVU_CH_BACKUP_S3_ENDPOINT/$prefix/') FORMAT TabSeparated")

if [ "$files" -eq 0 ]; then
  echo "backup finished but no objects were written to the destination" >&2
  exit 1
fi

printf 'ClickHouse backup completed: %s (%s objects)\n' "$prefix" "$files"
