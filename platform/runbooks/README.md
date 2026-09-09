# Runbooks

Procedimentos operacionais da camada de dados. Cinco são obrigatórios antes de
`ndovu-data-prd` receber tráfego real — os arquivos abaixo ainda **não foram
escritos**; esta lista é o backlog, não um índice do que existe.

| Arquivo | Gatilho | Deve responder |
|---------|---------|----------------|
| `clickhouse-disk.md` | PVC hot acima de 75% | Expandir PVC, forçar move de TTL para warm/cold, ou reduzir retenção — nesta ordem |
| `jetstream-backlog.md` | `num_pending` do consumer crescendo | Writer está de pé? ClickHouse aceita insert? Quanto tempo resta até o `MaxAge` descartar? |
| `postgres-restore.md` | Perda do control plane | Restore point-in-time; sem control plane a ingestão para inteira |
| `cold-tier-migration.md` | Troca de bucket ou de provedor S3 | Como mover parts do cold sem downtime de consulta |
| `storage-expansion.md` | Crescimento acima do tier planejado | Expandir PVC online, trocar StorageClass, rebalancear shards |

Cada um deve caber em uma página e ser executável por quem está de plantão às
3h da manhã sem conhecer o código: comandos concretos, saída esperada e o
critério de quando escalar para o time de desenvolvimento.

O `postgres-restore.md` é o único que exige **execução periódica**: restore
testado a cada trimestre. Um backup que nunca foi restaurado não é um backup.
