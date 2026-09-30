# Runbook — Migração do cold tier (troca de bucket ou provedor S3)

**Gatilho:** trocar o bucket R2 `ndovu-cold`, trocar a credencial, trocar o
provedor (R2 → S3 real), ou mudar o caminho do endpoint.

**Contexto:** o cold tier é um **disco montado** pelo ClickHouse
(`infra/clickhouse/storage-r2.xml`, tipo `s3`, endpoint/PaaS terminando em
`/ndovu-cold/`). As parts que o TTL moveu para frio têm caminho relativo ao
disco, gravado em `system.parts.disk_name`. **Regra de ouro do
DEPLOY-VPS.md §Cold tier:** a troca de endpoint **não** é mecanismo de
migração — não mova nem apague parts manualmente (`cp`/`rm`/`aws s3 cp` entre
buckets à mão). A movimentação legítima é feita pelo ClickHouse.

**Antes de qualquer coisa:** backup validado do ClickHouse + backup do
Postgres. Sem isso, não comece.

## 1. Verificar o que existe no cold hoje

```sql
SELECT disk_name, count() AS parts, round(sum(bytes_on_disk)/1e9, 2) AS gb
FROM system.parts WHERE table='trace_events' AND active
GROUP BY disk_name;
```

## 2. Caminho recomendado: coexistência de discos (sem downtime de consulta)

1. **Provisione o destino novo** com credencial exclusiva e escopo só no novo
   bucket (R2: crie o bucket antes).
2. **Registre o novo disco com outro nome** em
   `infra/clickhouse/storage-r2.xml` — mantenha o disco antigo no arquivo
   (novo `<disk>`, novo `<policy>` se precisar), **sem apagar** o antigo.
   Recrie o container: `... up -d clickhouse`.
   ```bash
   # confirma os dois discos montados
   clickhouse-client --query "SELECT name, path, type FROM system.disks"
   ```
3. **Mova as partições pelo ClickHouse** (a única forma suportada — a
   própria engine move e re-registra o path). **Sintaxe 24.8 testada
   (2026-09-30):** NÃO existe `PERSISTENT` — a part fica no destino
   automaticamente. Uma partição por vez, a mais fria primeiro:
   ```sql
   ALTER TABLE ndovu.trace_events MOVE PART '<part_id>' TO DISK '<novo_disk>';
   ```
   `<part_id>` vem de `system.parts` (passo 1). Monitore (colunas 24.8:
   `database, table, elapsed, target_disk_name, target_disk_path, part_name,
   part_size, thread_id`):
   ```sql
   SELECT elapsed, target_disk_name, part_name, part_size
   FROM system.moves ORDER BY elapsed DESC LIMIT 10;
   ```
4. **Refaça a consulta do passo 1** até que nenhuma part `active` aponte ao
   disco antigo.
5. **Só depois** remova o disco antigo do XML e recrie o container.

## 3. Troca simples de credencial do MESMO bucket

Nenhum dado muda de lugar: atualize
`NDOVU_CLICKHOUSE_S3_ACCESS_KEY`/`SECRET_KEY` no `.env.production` e recrie o
container (`... up -d clickhouse`). Valide com escrita nova + 1 consulta a
dado frio (última part em `system.parts` no disco cold).

## 4. Nunca faça

- Trocar o **caminho do endpoint** (ex.: de `/ndovu-cold/` para outro) com
  dados no cold sem mover parts primeiro — as parts existentes ficam
  inalcançáveis (consulta a dado antigo quebra).
- `DETACH`/`DROP` de partição "para liberar espaço" do cold.
- Copiar parts entre buckets por fora do ClickHouse.

## 5. Escalar para o time de desenvolvimento quando

- Aparecer erro de disco S3 no boot após a mudança de XML (credencial/endpoint
  incorretos — o serviço fica em restart loop);
- Consulta a dado com > 30 dias falhar após mover parts;
- `system.moves` acumular jobs com erro.
