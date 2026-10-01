FROM postgres:16-alpine

# age + aws-cli: cofre do Postgres (dump/restore).
#
# Cliente ClickHouse: NAO existe pacote Alpine `clickhouse-client` (erro real do
# 1o disparo) e a superficie de download oficial (builds.clickhouse.com,
# packages.clickhouse.com/static) esta 404/403. O binario vem entao da MESMA
# imagem oficial da stack (24.8), via --build-context abaixo.
#
# Buildx nao expande ${VAR} em COPY --from, entao o nome do contexto e fixo.
# O compose define `additional_contexts` para este build, e o
# tools/cd/prepare-backup-image.sh monta o mesmo contexto (idempotente): a
# imagem fica SEMPRE na mesma versao do servidor, sem depender de rede externa.
RUN apk add --no-cache age aws-cli

COPY --from=ndovu-clickhouse /usr/bin/clickhouse /usr/local/bin/clickhouse
RUN chmod 0755 /usr/local/bin/clickhouse \
 && ln -sf /usr/local/bin/clickhouse /usr/local/bin/clickhouse-client

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
