FROM postgres:16-alpine

# age + aws-cli: cofre do Postgres (dump/restore).
# O pacote Alpine `clickhouse-client` NAO EXISTE (foi o erro real do 1o disparo)
# e a imagem postgres nao traz nenhum client nativo. Entao o cliente e
# baixado do proprio binario oficial do ClickHouse; a stack sobe com
# --network host, entao 127.0.0.1:9000 e o ClickHouse da stack.
# 16-alpine ja traz `curl`; `update-ca-certificates` garante TLS no https.
RUN apk add --no-cache age aws-cli ca-certificates curl \
 && update-ca-certificates \
 && curl -fsSL https://builds.clickhouse.com/master/amd64/clickhouse \
      -o /usr/local/bin/clickhouse \
 && chmod 0755 /usr/local/bin/clickhouse \
 && ln -s /usr/local/bin/clickhouse /usr/local/bin/clickhouse-client

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
