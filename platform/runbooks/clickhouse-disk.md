# Runbook — Disco hot do ClickHouse acima de 75%

**Gatilho:** monitoramento do volume `chdata` (hot) do container `clickhouse`
acima de 75%, ou alerta `no space left on device` nos logs do writer.

**Contexto (leia antes de tocar):** a política `tiered`
(`infra/clickhouse/storage.xml` em dev; `storage-r2.xml` em prod) move o dado
sozinha por TTL: 0–7d hot → 7–30d warm → 30–90d cold (S3) → 90d+ delete. O
hot só deveria crescer por escrita dos últimos 7 dias + merges pendentes.
Disco hot cheio = 99% das vezes é **fusão atrasada**, não falta de espaço.
**Nunca delete ou mova parts manualmente** (`DETACH`, `rm`, `cp` do volume):
corrompe a contabilidade do ClickHouse.

**Acesso (VPS):**
```bash
cd /opt/ndovu
docker compose --env-file .env.production -f docker-compose.yml -f docker-compose.prod.yml \
  exec clickhouse clickhouse-client
```

## 1. Diagnóstico (em ordem)

```sql
-- uso por volume da política
SELECT name, path, keep_free_ratio,
       round((1 - free_bytes/total_bytes)*100, 1) AS used_pct
FROM system.disks;

-- partições recentes e tamanho (o que está no hot AGORA)
SELECT partition, name, round(bytes_on_disk/1e6, 1) AS mb,
       disk_name, active
FROM system.parts WHERE table = 'trace_events' AND active
ORDER BY partition DESC LIMIT 20;

-- merges pendentes (fusão atrasada = parts pequenas se acumulando)
SELECT count() AS pending_merges FROM system.merges;
SELECT count() AS small_parts
FROM system.parts WHERE table='trace_events' AND active AND bytes_on_disk < 10e6;
```

## 2. Ações, nesta ordem

**A. Aguardar a fundição (1ª ação; resolve a maioria dos casos).**
A fila de merge consome CPU — a 03h não incomoda ninguém. Reavalie em 1–2h
com a mesma query de `system.merges`. Não reinicie o ClickHouse com a fila ativa.

**B. Forçar a fusão das partições mais cheias** se a fila estiver parada:
```sql
-- verifica a fila de mutações/merges travada?
SELECT * FROM system.mutations WHERE is_done = 0;
-- força materialização do TTL na partição mais recente
ALTER TABLE ndovu.trace_events MATERIALIZE TTL IN PARTITION 'YYYYMMDD';
```

**C. Expandir o volume** se A+B não resolveram em 2h — na VPS
(Docker volume local; a hostpath pode crescer com o disco da VPS):
1. Expanda o disco da VPS (painel do provedor).
2. Redimensione o filesystem da hostpath dos volumes docker (ex.: `lvextend -l +100%FREE -r` no LVM, ou `xfs_growfs` conforme o fs da hostpath).
3. `docker volume ls` confirma que `chdata` é a hostpath expandida.

**D. Reduzir retenção — ÚLTIMO RECURSO, exige decisão do time de produto:**
encurtar o TTL (`INTERVAL 90 DAY DELETE`) apaga dado de todos os tenants e só
reverte com restore do backup ClickHouse (comando `BACKUP`/`RESTORE ... TO S3`
validado 2026-09-30).
Nunca rode a 03h por conta própria.

## 3. Escalar para o time de desenvolvimento quando

- `used_pct` ≥ 90% mesmo com `pending_merges = 0` (espaço está indo para outro lugar);
- `system.mutations is_done=0` acumulando sem avançar;
- erro de escrita no writer com disco < 85% (outra causa — abrir incidente de ingestão).
