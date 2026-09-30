FROM postgres:16-alpine

RUN apk add --no-cache age aws-cli

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]
