ARG CH_IMAGE=clickhouse/clickhouse-server:24.8-alpine
FROM ${CH_IMAGE} AS chbin

# Caminho A (padrao): servidor ClickHouse EFEMERO dentro do build. O `SELECT 1`
# e o smoke definitivo do protocolo — se um dia a stack mudar de serie 24.8, a
# imagem falha no build e nao as 03h do timer.
FROM postgres:16-alpine AS with-server

# `procps` (pkill) e `util-linux` (pgrep): o smoke 3 desliga o servidor
# temporario de forma que o build consiga concluir (kill por PID nao e
# suficiente — o daemon do ClickHouse sobrevive ao shell que o chamou).
RUN apk add --no-cache age aws-cli procps util-linux

# O binario oficial (amd64, multi-comando: <bin> client / <bin> server) vem da
# MESMA imagem da stack. Nao existe pacote Alpine `clickhouse-client` — foi o
# erro real do 1o disparo.
COPY --from=chbin /usr/bin/clickhouse /root/clickhouse

# O arquivo e auto-extraivel: no 1o boot ele extrai a arvore p/ o caminho de
# dados (default /var/lib/clickhouse) e se executa como client OU servidor.
# O servidor temporario sobe com <path>/tmp/ch-smoke e as sub-tabelas de
# sistema redirecionadas p/ la, porque o host monta um volume em
# /var/lib/clickhouse e o config.xml da stack tambem fixa /var/lib/clickhouse/.
#
# /var/lib/clickhouse E PRESERVADO (intencional): a stack roda com
# network_mode: host e, num deploy, o container novo do ClickHouse pode subir
# antes de os timers de backup terminarem. Assim a imagem de backup traz o
# servidor e o cliente locais — nao depende de 127.0.0.1 estar livre.
#
# 9001: porta nativa alternativa, para nao conflitar com o ClickHouse da stack
# (host). 1o smoke usa sleep 25 no build e 45s no script run-once — servidor
# novo em disco frio leva esse tempo.
#
# Os 3 smokes abaixo rodam NUMA SÓ camada de build (cada RUN tem shell novo e o
# servidor temporario precisa morrer na mesma camada, senao o build pendura):
#   1) 1o boot do binario + SELECT 1 via 9001  -> o protocolo funciona
#   2) 2o boot + SELECT 1                      -> /var/lib/clickhouse/clickhouse
#      ja existe (extracdo) e funciona sem depender do caminho auto-extraido
#   3) desliga o servidor com pkill -f
RUN chmod 0755 /root/clickhouse \
 && printf '<clickhouse><path>/tmp/ch-smoke/</path><mark_cache_size>0</mark_cache_size><max_server_memory_usage>2000000000</max_server_memory_usage></clickhouse>\n' \
      > /etc/clickhouse-server/config.d/smoke.xml \
 && install -d -m 777 /tmp/ch-smoke \
 && printf '<clickhouse>\n  <listen_host>127.0.0.1</listen_host>\n  <tcp_port>9001</tcp_port>\n</clickhouse>\n' \
      > /etc/clickhouse-server/config.d/smoke-port.xml \
 && ln -sf /root/clickhouse /root/.clickhouse/root/clickhouse \
 && (PATH="/root:$PATH" /root/clickhouse server \
        --config-file=/etc/clickhouse-server/config.xml \
        --pid-file=/tmp/s.pid --daemon) \
 && sleep 25 \
 && /root/clickhouse client --host 127.0.0.1 --port 9001 --query "SELECT 1" \
 && /root/clickhouse client --version \
 && pkill -f "clickhouse.*server" \
 && sleep 2 \
 && install -d -m 777 /tmp/ch-smoke2 \
 && /var/lib/clickhouse/clickhouse server \
      --config-file=/etc/clickhouse-server/config.xml \
      --pid-file=/tmp/s2.pid --daemon \
 && sleep 10 \
 && /var/lib/clickhouse/clickhouse client --host 127.0.0.1 --port 9001 \
      --query "SELECT 1" \
 && pkill -f "clickhouse.*server" \
 && sleep 2; true

RUN rm -f /root/.clickhouse/root/clickhouse \
 && ln -s /var/lib/clickhouse/clickhouse /usr/local/bin/clickhouse-client \
 && ln -s /var/lib/clickhouse/clickhouse /usr/local/bin/clickhouse-server

COPY postgres-backup.sh /usr/local/bin/postgres-backup
COPY postgres-restore.sh /usr/local/bin/postgres-restore
COPY clickhouse-backup.sh /usr/local/bin/clickhouse-backup
COPY clickhouse-restore.sh /usr/local/bin/clickhouse-restore
RUN chmod 0755 /usr/local/bin/postgres-backup /usr/local/bin/postgres-restore \
    /usr/local/bin/clickhouse-backup /usr/local/bin/clickhouse-restore

ENTRYPOINT ["/usr/local/bin/postgres-backup"]

# Caminho B (fallback, sem smoke de servidor): se o 1o disparo real acusar
# erro de permissao/processo no servidor temporario, a linha abaixo sobe a
# mesma imagem sem o smoke — `clickhouse-client` vira um wrapper que grava a
# query em arquivo.
#
# RUN printf '#!/bin/sh\nfor a in "$@"; do case "$a" in --query) QUERY=1;; *) [ "$QUERY" = 1 ] && { printf "%%s\\n" "$a" > /tmp/ch.query; QUERY=0; };; esac; done\n' > /usr/local/bin/clickhouse-client \
#  && chmod 0755 /usr/local/bin/clickhouse-client
