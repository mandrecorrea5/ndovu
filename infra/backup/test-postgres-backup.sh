#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
env_file="${NDOVU_ENV_FILE:-$repo_root/.env.production}"
if [[ ! -r "$env_file" ]]; then
  echo "production env file is not readable: $env_file" >&2
  exit 2
fi

test_id="ndovu-backup-test-$$"
tmpdir="$(mktemp -d)"
backup_uri=""
endpoint=""
bucket=""

cleanup() {
  if [[ -n "$backup_uri" ]]; then
    compose run -T --rm --no-deps --entrypoint aws postgres-backup \
      --no-cli-pager s3api delete-object \
      --endpoint-url "$endpoint" --region auto \
      --bucket "$bucket" --key "${backup_uri#s3://*/}" \
      >/dev/null 2>&1 || echo "WARNING: synthetic backup object cleanup failed: $backup_uri" >&2
  fi
  compose down --remove-orphans >/dev/null 2>&1 || true
  rm -f -- "$tmpdir/identity.txt"
  rmdir -- "$tmpdir" 2>/dev/null || true
}
trap cleanup EXIT

export NDOVU_PG_BACKUP_AGE_RECIPIENT=age1temporaryplaceholder
compose() {
  docker compose --project-name "$test_id" --env-file "$env_file" \
    -f "$repo_root/docker-compose.yml" \
    -f "$repo_root/docker-compose.prod.yml" \
    -f "$repo_root/infra/backup/backup-test-compose.yml" \
    --profile backup "$@"
}

compose build postgres-backup
compose run -T --rm --no-deps --entrypoint age-keygen postgres-backup \
  >"$tmpdir/identity.txt" 2>/dev/null
chmod 600 "$tmpdir/identity.txt"
export NDOVU_PG_BACKUP_AGE_RECIPIENT="$(
  compose run -T --rm --no-deps \
    -v "$tmpdir/identity.txt:/run/age-identity:ro" \
    --entrypoint age-keygen postgres-backup -y /run/age-identity 2>/dev/null
)"

compose up -d postgres postgres-restore-test
for service in postgres postgres-restore-test; do
  ready=false
  for _ in $(seq 1 60); do
    if compose exec -T "$service" pg_isready -U backup_test -d backup_test >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then
    echo "synthetic PostgreSQL service did not become ready: $service" >&2
    exit 1
  fi
done
compose exec -T postgres psql -U backup_test -d backup_test \
  -v ON_ERROR_STOP=1 \
  -c 'CREATE TABLE backup_roundtrip (id integer PRIMARY KEY, marker text NOT NULL)' \
  -c "INSERT INTO backup_roundtrip VALUES (1, 'ndovu synthetic backup restore verified')"

values="$(python3 - "$env_file" <<'PY'
from pathlib import Path
import sys
values = {}
for line in Path(sys.argv[1]).read_text().splitlines():
    if line and not line.lstrip().startswith("#") and "=" in line:
        key, value = line.split("=", 1)
        values[key] = value.strip().strip('"').strip("'")
print(values.get("NDOVU_PG_BACKUP_S3_ENDPOINT", ""))
print(values.get("NDOVU_PG_BACKUP_BUCKET", ""))
PY
)"
endpoint="$(printf '%s\n' "$values" | sed -n '1p')"
bucket="$(printf '%s\n' "$values" | sed -n '2p')"

backup_output="$(compose run -T --rm postgres-backup)"
printf '%s\n' "$backup_output" | grep -E 'PostgreSQL backup uploaded:' | tail -1
backup_uri="$(printf '%s\n' "$backup_output" | grep -Eo 's3://[^ ]+' | tail -1)"
if [[ -z "$backup_uri" ]]; then
  echo "backup command did not return an object URI" >&2
  exit 1
fi

compose run -T --rm --no-deps \
  -v "$tmpdir/identity.txt:/run/age-identity:ro" \
  -e PGHOST=postgres-restore-test \
  -e PGPORT=5432 \
  -e PGUSER=backup_test \
  -e PGPASSWORD=backup_test_only \
  -e PGDATABASE=backup_test \
  -e AGE_IDENTITY_FILE=/run/age-identity \
  --entrypoint /usr/local/bin/postgres-restore \
  postgres-backup "$backup_uri"

result="$(compose exec -T postgres-restore-test psql -U backup_test -d backup_test \
  -At -v ON_ERROR_STOP=1 \
  -c 'SELECT marker FROM backup_roundtrip WHERE id = 1')"
if [[ "$result" != "ndovu synthetic backup restore verified" ]]; then
  echo "restored row does not match synthetic test value" >&2
  exit 1
fi

echo "Synthetic encrypted backup/restore round trip passed; test objects and containers will be removed."
