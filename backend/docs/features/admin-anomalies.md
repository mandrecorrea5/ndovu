# Detecção de anomalia (`/admin/anomalies`)

## Para que serve

Enquanto [Alertas](admin-alerts.md) usa threshold fixo, essa tela
detecta desvios estatísticos: compara a **janela atual** com o
comportamento histórico da **mesma hora + mesmo dia da semana** nas
últimas N semanas. Se o desvio ultrapassa `sensitivity` (em
`stddev`s), dispara notificação — sem precisar adivinhar número
mágico. Também lista as **detecções recentes** com z-score real e
status de entrega.

## Onde fica

- Rota: `/admin/anomalies`
- Arquivo: `dashboard/src/app/admin/anomalies/page.tsx`
- Endpoints:
  - `GET /v1/admin/anomaly-rules`
  - `POST /v1/admin/anomaly-rules`
  - `PATCH /v1/admin/anomaly-rules/{id}`
  - `DELETE /v1/admin/anomaly-rules/{id}`
  - `GET /v1/admin/anomaly-detections`
- Papel mínimo: **admin**

## Como usar

1. Abra **Detecção de anomalia**. Duas seções: **Regras** (config) e
   **Detecções recentes** (últimas 20, refresh a cada 60s).
2. Clique **Nova regra** e preencha:
   - **Nome** (ex.: `spike-erro-portal`).
   - **App** — vazio = todos.
   - **Métrica**: `Contagem de erros`, `Contagem de eventos` ou
     `Taxa de erro (erros/eventos)`.
   - **Direção**: `Spike (acima da média)`, `Silêncio (abaixo da
     média)` ou `Ambos`.
   - **Janela atual (minutos)** — 1 a 240.
   - **Baseline (semanas)** — 1 a 12 semanas atrás para calcular a
     média (mesma hora + weekday).
   - **Sensitivity** — slider de 1 a 6σ. 2σ = mais sensível, 4σ =
     mais conservador.
   - **Silêncio (segundos)** — impede re-disparo no intervalo.
   - **Canal** + **URL de destino**.
   - **Ativa (avaliada a cada 5 minutos)**.
3. **editar** reabre o modal; **remover** exclui (com confirmação).
4. Role até **Detecções recentes** para acompanhar dispares reais.

## O que você vê

- **Regras**: tabela com **Nome**, **Escopo** (`app` ou `*`),
  **Métrica**, **Janela** (Xmin), **Baseline** (Nw), **Sensitivity**
  (`3.0σ`), **Direção** (`↑ spike` / `↓ silêncio` / `↕ ambos`),
  **Canal**, **Status**, **Ações**.
- **Detecções recentes**: **Quando**, **Regra**, **Direção**,
  **Atual**, **Baseline** (`avg ± stddev`), **Z-score** (colorido:
  vermelho para `|z|>5`), **Notify** (`✓ entregue` / `✕ falhou` com
  tooltip do erro).
- Mensagem vazia: "Nenhuma anomalia detectada ainda. O avaliador
  roda a cada 5 minutos no writer."

## Como demonstrar

> "Diferente do alerta clássico, ele aprende o comportamento normal.
> Sexta às 15h o portal sempre tem pico de erros de checkout? Essa
> hora entra no baseline. Um pico fora do padrão dispara — sem thresh
> mágico, sem falso positivo em horário de rush."

Roteiro de 60s:
1. Mostre uma regra ativa: `spike-erro-portal`, métrica
   `error_count`, baseline `4w`, sensitivity `3σ`, direção `↑ spike`.
2. Aponte na tabela de detecções: coluna **Baseline** mostra
   `12.30 ± 2.10`, **Atual** `48.00`, **Z-score** `+16.98σ` em
   vermelho.
3. Explique o cálculo: `(atual - avg) / stddev`; se `|z| >
   sensitivity`, dispara.
4. Rode o script de anomalia (`tools/seed --spike`) para gerar
   detecção nova em ~5 minutos.
5. Mostre `✓ entregue` na coluna **Notify** ou o tooltip do erro
   quando o webhook cai.

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link.
- Admin só vê regras/detecções dos apps do próprio tenant —
  `tenantScope` filtra tanto o CRUD quanto a listagem de detections.
- `is_super` vê tudo cross-tenant.

## Dependências

- Precisa de **eventos históricos suficientes** — pelo menos
  `baselineWeeks` semanas de dados na mesma hora/weekday. Sem isso, a
  média/stddev não estabiliza e a regra fica ruidosa ou silenciosa.
- O avaliador roda no **writer** a cada 5 minutos. Se o writer estiver
  parado, não há detecção.
- Slack ou webhook precisa estar acessível pela rede do backend.

## Perguntas frequentes

- **"Como escolher sensitivity?"**  
  `3σ` cobre ~99.7% do comportamento normal, é o default sensato. Suba
  para `4σ` se estiver com falso positivo; desça para `2σ` se está
  perdendo eventos reais.
- **"Baseline `4w` e eu só tenho 2 semanas de histórico?"**  
  A regra usa o que tiver, mas o stddev fica instável. Espere
  acumular pelo menos as N semanas configuradas para dados confiáveis.
- **"Anomalia notifica no Slack igual ao alerta?"**  
  Sim. O payload é diferente (traz z-score, atual, baseline), mas o
  canal é o mesmo.
- **"E se a regra estiver desativada?"**  
  Não é avaliada — mas fica listada e pode ser reativada sem perder
  histórico de detecções passadas.

## Referências

- Página: `dashboard/src/app/admin/anomalies/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listAnomalyRules`,
  `createAnomalyRule`, `updateAnomalyRule`, `deleteAnomalyRule`,
  `listAnomalyDetections`
- Avaliador backend: `backend/internal/usecase/anomaly`
- Docs relacionadas: [admin-alerts.md](admin-alerts.md),
  [admin-audit-log.md](admin-audit-log.md)
