FROM postgres:16-alpine

# age + aws-cli: cofre do Postgres (dump/restore).
#
# Cliente ClickHouse — por que NAO as 3 fontes "obvias":
#   1. `apk add clickhouse-client`: NAO EXISTE em Alpine (foi o 1o erro real).
#   2. 24.8 da stack: /usr/bin/clickhouse dela e SO servidor (a 24.12 sim traz
#      o client, e 24.12 + servidor 24.8 da erro de handshake).
#   3. download da superficie publica: packages.clickhouse.com esta 403/404.
#
# Fonte certa: builds.clickhouse.com/master/amd64/clickhouse (200, 217MB), que
# e o binario oficial multi-comando (client + server) — 2o erro real de
# tentativa foi so um caminho errado da minha parte.
#
# postgres:16-alpine NAO traz curl, entao ele entra no apk junto.
RUN apk add --no-cache age aws-cli curl ca-certificates \
 && curl -fsSL https://builds.clickhouse.com/master/amd64/clickhouse \
      -o /usr/local/bin/clickhouse \
 && chmod 0755 /usr/local/bin/clickhouse \
 && ln -sf /usr/local/bin/clickhouse /usr/local/bin/clickhouse-client \
 && clickhouse-client --version

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
