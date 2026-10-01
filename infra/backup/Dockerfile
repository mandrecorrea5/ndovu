FROM postgres:16-alpine

# age + aws-cli: cofre do Postgres (dump/restore).
# Cliente ClickHouse: NAO existe pacote Alpine `clickhouse-client` (erro real do
# 1o disparo) e a imagem postgres nao traz nenhum client. Baixamos o binario
# estatico oficial do ClickHouse e o expomos como `clickhouse-client`.
# A stack sobe com network_mode: host, entao os scripts falam com o ClickHouse
# via 127.0.0.1:9000.
ARG CH_VERSION=24.8.14.24
RUN apk add --no-cache age aws-cli ca-certificates curl \
 && curl -fsSL "https://packages.clickhouse.com/static/clickhouse-${CH_VERSION}-x86_64" \
      -o /usr/local/bin/clickhouse \
 && chmod 0755 /usr/local/bin/clickhouse \
 && ln -s /usr/local/bin/clickhouse /usr/local/bin/clickhouse-client \
 && clickhouse-client --version

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
