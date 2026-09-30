# Runbooks

Procedimentos operacionais da camada de dados — escritos e revisados
(2026-09-30). Comandos na forma da trilha atual (VPS + Docker Compose); a
trilha OpenShift manterá a mesma lógica com `kubectl`/`oc`.

| Arquivo | Gatilho |
|---------|---------|
| [`clickhouse-disk.md`](./clickhouse-disk.md) | Disco hot > 75% |
| [`jetstream-backlog.md`](./jetstream-backlog.md) | `num_pending` do consumer crescendo |
| [`postgres-restore.md`](./postgres-restore.md) | Perda do control plane — **restore testado a cada trimestre** |
| [`cold-tier-migration.md`](./cold-tier-migration.md) | Troca de bucket/provedor S3 do cold |
| [`storage-expansion.md`](./storage-expansion.md) | Crescimento acima do tier planejado |

Deploy/update/rollback da aplicação:
[`docs/RUNBOOK-DEPLOY.md`](../../docs/RUNBOOK-DEPLOY.md).

Regras dos runbooks: uma página, comandos concretos, saída esperada e o
critério de escalar para o desenvolvimento. `postgres-restore.md` é o único
com **execução periódica obrigatória** (trimestral).
