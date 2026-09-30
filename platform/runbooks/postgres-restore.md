# Runbook — Restore do PostgreSQL (control plane)

**Gatilho:** perda/corrupção do Postgres (usuários, chaves de API, apps,
permissões). **Consequência sem restore:** a ingestão **para inteira** — sem
control plane a API não valida `X-Api-Key` — e ninguém loga no dashboard.
Prioridade máxima.

**Cadência obrigatória:** este runbook precisa ser **executado a cada
trimestre** em ambiente temporário. Backup que nunca voltou, não é backup.
(Único runbook com essa exigência.)

## Ferramentas (já construídas no repositório)

- Backup diário 02:15 UTC → `s3://ndovu-prod-backups/postgres/YYYY/MM/DD/*.dump.age`
  (cifrado com age; **identidade privada no cofre**, nunca na VPS).
- Script de restore: `postgres-restore` (entrypoint do serviço `postgres-backup`
  — imagem `infra/backup/Dockerfile`). Ele baixa + descriptografa em stream e
  roda `pg_restore --single-transaction`. **Não faz DROP** e **não aponta** para
  produção automaticamente.

## 1. Listar backup disponível

```bash
cd /opt/ndovu
docker compose --env-file .env.production -f docker-compose.yml -f docker-compose.prod.yml \
  --profile backup run --rm --no-deps --entrypoint aws postgres-backup \
  s3 ls s3://ndovu-prod-backups/postgres/ --recursive \
  --endpoint-url "$NDOVU_PG_BACKUP_S3_ENDPOINT" --region auto
```

## 2. Restore SEMPRE primeiro em destino temporário

Suba um Postgres descartável na rede do compose (ex.: `docker run --rm -d
--name ndovu-pg-restore --network ndovu_default -e POSTGRES_USER=restore
-e POSTGRES_PASSWORD=... -e POSTGRES_DB=ndovu_restore postgres:16-alpine`),
depois:

```bash
# a identidade age do cofre, montada SOMENTE neste container, somente leitura
docker compose --env-file .env.production -f docker-compose.yml -f docker-compose.prod.yml \
  --profile backup run --rm --no-deps \
  -v /CAMINHO-COFRE/pg-backup-identity.txt:/run/secrets/pg-identity.txt:ro \
  -e PGHOST=ndovu-pg-restore -e PGPORT=5432 \
  -e PGUSER=restore -e PGPASSWORD='<senha-temporaria>' \
  -e PGDATABASE=ndovu_restore \
  -e AGE_IDENTITY_FILE=/run/secrets/pg-identity.txt \
  --entrypoint /usr/local/bin/postgres-restore \
  postgres-backup \
  s3://ndovu-prod-backups/postgres/<YYYY/MM/DD/arquivo.dump.age>
```

Saída esperada: `PostgreSQL backup restored from s3://... into database ...`.

## 3. Validar o restore (checklist mínima)

```sql
-- no destino temporário:
SELECT count(*) FROM users;                          -- bate com o esperado?
SELECT count(*) FROM api_keys;                       -- > 0
SELECT count(*) FROM apps;                           -- > 0
SELECT email, role, active FROM users LIMIT 20;      -- admins ativos presentes
```

Valide **login** (dashboard apontando a API para este destino via
`NDOVU_POSTGRES_URL` temporário) e **uma chave de ingestão real**
(1 evento → 202). Usuários e chaves **não existem** em lugar além deste banco —
não promova sem esses 2 testes.

## 4. Promover para produção (janela de manutenção, time no chat)

1. `... stop api writer` (congela a ingestão).
2. Dump de segurança do estado atual (`pg_dump` local) antes de qualquer overwrite.
3. Restore no destino de produção com o MESMO script (valide PG* apontando
   ao production por 2x antes de dar enter).
4. `... up -d postgres` (se foi recriado) → `... up -d api writer`.
5. Smoke: `/health` 200, login ok, 1 evento 202, evento visível no dashboard
   em ≤ 1 min.

## 5. Escalar para o time de desenvolvimento quando

- `pg_restore` acusar divergência de schema/versão;
- contagens de usuários/chaves não baterem com o esperado;
- qualquer dúvida sobre promover — este runbook **nunca** se resolve sozinho
  às 03h sem 2ª pessoa.
