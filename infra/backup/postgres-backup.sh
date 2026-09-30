#!/bin/sh
set -eu
set -o pipefail
umask 077

: "${PGHOST:?PGHOST is required}"
: "${PGPORT:?PGPORT is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PGDATABASE:?PGDATABASE is required}"
: "${AWS_ACCESS_KEY_ID:?AWS_ACCESS_KEY_ID is required}"
: "${AWS_SECRET_ACCESS_KEY:?AWS_SECRET_ACCESS_KEY is required}"
: "${NDOVU_BACKUP_S3_ENDPOINT:?NDOVU_BACKUP_S3_ENDPOINT is required}"
: "${NDOVU_BACKUP_BUCKET:?NDOVU_BACKUP_BUCKET is required}"
: "${NDOVU_BACKUP_AGE_RECIPIENT:?NDOVU_BACKUP_AGE_RECIPIENT is required}"

case "$NDOVU_BACKUP_AGE_RECIPIENT" in
  age1*) ;;
  *)
    echo "NDOVU_BACKUP_AGE_RECIPIENT must be an age public recipient (age1...)" >&2
    exit 2
    ;;
esac

case "$NDOVU_BACKUP_S3_ENDPOINT" in
  https://*) ;;
  *)
    echo "NDOVU_BACKUP_S3_ENDPOINT must use HTTPS" >&2
    exit 2
    ;;
esac

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
filename="ndovu-postgres-${timestamp}.dump.age"
object_key="postgres/$(date -u '+%Y/%m/%d')/$filename"
encrypted_file="$workdir/$filename"

pg_dump \
  --host="$PGHOST" \
  --port="$PGPORT" \
  --username="$PGUSER" \
  --dbname="$PGDATABASE" \
  --format=custom \
  --no-owner \
  --no-acl \
  | age --encrypt --recipient "$NDOVU_BACKUP_AGE_RECIPIENT" --output "$encrypted_file"

if [ ! -s "$encrypted_file" ]; then
  echo "encrypted PostgreSQL dump is empty" >&2
  exit 1
fi

aws --no-cli-pager s3 cp "$encrypted_file" \
  "s3://$NDOVU_BACKUP_BUCKET/$object_key" \
  --endpoint-url "$NDOVU_BACKUP_S3_ENDPOINT" \
  --region auto \
  --only-show-errors

aws --no-cli-pager s3api head-object \
  --endpoint-url "$NDOVU_BACKUP_S3_ENDPOINT" \
  --region auto \
  --bucket "$NDOVU_BACKUP_BUCKET" \
  --key "$object_key" \
  >/dev/null

printf 'PostgreSQL backup uploaded: s3://%s/%s (%s bytes, age encrypted)\n' \
  "$NDOVU_BACKUP_BUCKET" "$object_key" "$(wc -c < "$encrypted_file" | tr -d ' ')"
