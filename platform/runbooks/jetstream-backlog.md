# Runbook — Backlog do JetStream (stream `NDOVU` crescendo)

**Gatilho:** `num_pending` (consumidor `ndovu-writer`) ou
`state.Messages`/`state.Bytes` do stream crescendo de forma sustentada.

**Contexto:** a ingestão **nunca** espera o banco — a API publica no stream e
retorna 202. O writer consome em lote e só faz ack após o insert no
ClickHouse. Backlog = o writer não está acompanhando (ou está parado).
**Prazo crítico:** o stream tem `MaxAge` = 48h (ajustável via
`NDOVU_STREAM_MAX_AGE_HOURS` na config). **Mensagem mais velha que 48h é
descartada para sempre** — backlog > 48h de idade = perda de dados. A DLQ
(`ndovu.dlq.<app>`) fica no MESMO stream e consome a mesma retenção.

**Acesso (VPS):**
```bash
cd /opt/ndovu
docker compose --env-file .env.production -f docker-compose.yml -f docker-compose.prod.yml \
  exec nats nats stream info NDOVU
docker compose --env-file .env.production -f docker-compose.yml -f docker-compose.prod.yml \
  exec nats nats consumer info NDOVU ndovu-writer
```

## 1. Diagnóstico (em ordem)

```
nats stream info NDOVU
  → State.Messages / State.Bytes : tamanho real
  → FirstSeq vs LastSeq          : idade do buraco
nats consumer info NDOVU ndovu-writer
  → NumPending                   : backlog efetivo
  → DeliverPending / AckPending  : o que está "em voo" agora
```

```bash
# 1. O writer está vivo?
docker compose -f docker-compose.yml -f docker-compose.prod.yml \
  --env-file .env.production ps writer

# 2. O ClickHouse aceita insert? (health da API valida a conexão)
docker compose -f docker-compose.yml -f docker-compose.prod.yml \
  --env-file .env.production exec api wget -qO- http://localhost:8080/health

# 3. Erros do writer (insert recusado → DLQ, inserts falhando em loop)
docker compose -f docker-compose.yml -f docker-compose.prod.yml \
  --env-file .env.production logs --tail=100 writer | grep -iE "error|dlq|timeout"
```

## 2. Decisão

| Sintoma | Ação |
|---|---|
| Writer parado/restartando | `... up -d writer`; se reiniciar em loop, veja logs do ClickHouse antes de subir 2 replicas |
| ClickHouse recusando insert (disk, timeout) | **Não é problema do stream** — rode `clickhouse-disk.md`. Backlog é consequência |
| Writer vivo, ClickHouse ok, backlog caindo lentamente | Normal (picos). Aguarde `NumPending` zerar; **monitorar a idade**: `nats consumer info ... -f json` → campo mais antigo vs agora |
| Backlog parado de cair com writer ok | Verificar se um único app está despejando: `nats stream info NDOVU` não quebra por subject — para triagem rápida, cheque a API por chave de app (`/metrics`: `ndovu_ingest_*`) |

## 3. Backlog se aproximando de 48h (risco de perda)

1. **Comprove a idade:** `nats stream info NDOVU -w` → idade da 1ª msg vs `MaxAge`.
2. **Aumente o MaxAge ANTES de descartar** — na host, no `.env.production`/compose adicione em `api` e `writer` a env `NDOVU_STREAM_MAX_AGE_HOURS` (ex.: `72`):
   ```bash
   ... up -d api writer   # recria os dois; EnsureStream aplica a mudança
   ```
   Confirme aplicado com `nats stream info NDOVU` → `Max Age`.
3. **Se a idade já estourou e mensagens estão sendo descartadas:** capture
   quais apps/janelas foram perdidos (logs do writer + `nats stream info`)
   e registre no incidente — clientes afetados precisam saber.

## 4. Escalar para o time de desenvolvimento quando

- Writer saudável + ClickHouse saudável + `NumPending` crescendo > 30 min;
- DLQ crescendo (inserts sendo rejeitados — ver logs do writer);
- Necessidade de **replay** de DLQ ou reset de consumidor (toda operação de
  `nats consumer` / `erase` é escrita, **nunca** rode sem o time).
