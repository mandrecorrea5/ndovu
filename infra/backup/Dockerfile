FROM postgres:16-alpine

RUN apk add --no-cache age aws-cli

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
# clickhouse-client (TCP nativo 9000) para os 2 scripts CH — a mesma imagem
# não o traz; a API fala ClickHouse por Go, mas o backup precisa do client.
RUN apk add --no-cache clickhouse-client \
 && chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
