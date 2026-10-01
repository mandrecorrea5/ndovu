# Backup/restore do ClickHouse: os scripts so precisam de sh + clickhouse-client
# (o servidor e quem fala com o S3 via BACKUP/RESTORE ... s3(...)).
#
# Imagem Ubuntu (glibc) da MESMA versao do servidor da stack: traz o client
# nativo, sem download externo e sem diferenca de versao no handshake. A
# variante -alpine nao serve: o binario oficial do ClickHouse e glibc.
FROM clickhouse/clickhouse-server:24.8

COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore \
 && clickhouse-client --version

ENTRYPOINT ["/usr/local/bin/clickhouse-backup"]
