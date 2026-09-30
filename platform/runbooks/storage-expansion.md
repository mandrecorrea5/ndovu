# Runbook — Expansão de storage (hot/warm)

**Gatilho:** crescimento sustentado acima do tier planejado (hot ou warm)
com o `clickhouse-disk.md` já executado sem resolver — ou projeção de
capacidade indicando que o volume atual não aguenta até a próxima janela
manhã.

**Contexto VPS/Compose:** os volumes `chdata` (hot) e `chdata-warm` são
armazenamento **local da host** (named volumes Docker → hostpath em
`/var/lib/docker/volumes/`). Não existe "StorageClass" nem PVC — a expansão é
no **disco da VM + filesystem da hostpath**. O cold tier é S3 (R2) e é
elástico: se o problema for frio, expanda o bucket (nada a fazer — ele não
tem cota no nosso plano).

**Antes:** backup ClickHouse + Postgres. A operação é online para
consulta/escrita na maior parte do caminho, mas exige janela acordada.

## 1. Confirmar em qual tier está o crescimento

```sql
SELECT disk_name, round(sum(bytes_on_disk)/1e9, 2) AS gb, count() AS parts
FROM system.parts WHERE table='trace_events' AND active
GROUP BY disk_name;
-- e no host:
-- docker run --rm -v /var/lib/docker/volumes:/host alpine df -h /host
```

## 2. Caminho da expansão (VPS, hostpath local)

1. **Expanda o disco da VM** pelo painel do provedor (Hetzner/DO/etc.).
   Na maioria dos provedores isso não reinicia a VM.
2. **Redimensione o filesystem da hostpath dos volumes docker:**
   ```bash
   lsblk                                   # identifica o device novo
   # LVM:
   sudo pvresize /dev/<dev>
   sudo lvextend -l +100%FREE -r /dev/<vg>/<lv>   # -r já estende o fs
   # ext4 sem LVM: sudo resize2fs /dev/<dev>
   # xfs sem LVM:  sudo xfs_growfs <mountpoint>
   df -h /var/lib/docker
   ```
3. **Nenhum container precisa reiniciar** — o ClickHouse enxerga o espaço
   novo na hora (verifique: `SELECT ... FROM system.disks` → `total_bytes`).
4. **Reavalie 24h** — a fusão + TTL vão redistribuir; a pressão deve ceder.

## 3. Alternativa sem expandir disco: rebalancear tiers

Se o hot estourou mas o warm está folgado, a solução é mover a fundição, não
comprar disco: force a materialização do TTL nas partições do hot (ver
`clickhouse-disk.md` §2B).

## 4. Nunca faça

- Reduzir tamanho de volume/ disco já montado com dados (é destrutivo).
- Mover `/var/lib/docker/volumes/...` para outro mount sem parar os serviços
  com dados consistentes (backup primeiro).
- Adicionar um 2º disco sem plano de política — a policy `tiered` é
 declarado em 1 arquivo (`infra/clickhouse/storage-r2.xml`); mudança de
 topologia de discos é mudança de schema-operacional, com 2ª pessoa.

## 5. Escalar para o time de desenvolvimento quando

- O provedor exigir migration/backup para expandir (janela maior);
- `system.disks` não refletir o tamanho novo (hostpath errada / overlay);
- Necessidade de mover `/var/lib/docker` de mount — operação com risco de
  perda, só com time no chat e backup confirmado.
