#!/bin/sh
set -eu
set -o pipefail
umask 077

object_uri=${1:-}
if [ -z "$object_uri" ]; then
  echo "usage: postgres-restore s3://bucket/object-key" >&2
  exit 2
fi

: "${PGHOST:?PGHOST is required}"
: "${PGPORT:?PGPORT is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PGDATABASE:?PGDATABASE is required}"
: "${AWS_ACCESS_KEY_ID:?AWS_ACCESS_KEY_ID is required}"
: "${AWS_SECRET_ACCESS_KEY:?AWS_SECRET_ACCESS_KEY is required}"
: "${NDOVU_BACKUP_S3_ENDPOINT:?NDOVU_BACKUP_S3_ENDPOINT is required}"
: "${AGE_IDENTITY_FILE:?AGE_IDENTITY_FILE is required}"
if [ ! -r "$AGE_IDENTITY_FILE" ]; then
  echo "AGE_IDENTITY_FILE is not readable" >&2
  exit 2
fi
case "$NDOVU_BACKUP_S3_ENDPOINT" in
  https://*) ;;
  *)
    echo "NDOVU_BACKUP_S3_ENDPOINT must use HTTPS" >&2
    exit 2
    ;;
esac
case "$object_uri" in
  s3://*/*) ;;
  *)
    echo "backup URI must be s3://bucket/object-key" >&2
    exit 2
    ;;
esac

bucket=${object_uri#s3://}
bucket=${bucket%%/*}
object_key=${object_uri#s3://*/}
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT HUP INT TERM
encrypted_file="$workdir/backup.dump.age"

aws --no-cli-pager s3 cp "$object_uri" "$encrypted_file" \
  --endpoint-url "$NDOVU_BACKUP_S3_ENDPOINT" \
  --region auto \
  --only-show-errors

age --decrypt --identity "$AGE_IDENTITY_FILE" "$encrypted_file" \
  | pg_restore \
      --host="$PGHOST" \
      --port="$PGPORT" \
      --username="$PGUSER" \
      --dbname="$PGDATABASE" \
      --exit-on-error \
      --no-owner \
      --no-acl \
      --single-transaction

printf 'PostgreSQL backup restored from s3://%s/%s into database %s\n' \
  "$bucket" "$object_key" "$PGDATABASE"
