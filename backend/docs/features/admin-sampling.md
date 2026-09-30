# Sampling adaptativo (`/admin/sampling`)

## Para que serve

Reduz o volume gravado no ClickHouse **antes** da persistência —
mantém uma fração dos eventos por regra, sem tocar no SDK do cliente.
Cada regra define **app + tipo de evento + taxa (0-1)**, e a regra
mais específica ganha. Erros passam por **bypass** por padrão
(`keepErrors=true`), preservando o sinal crítico mesmo com sampling
agressivo no restante — a decisão é server-side, dinâmica, sem
deploy.

## Onde fica

- Rota: `/admin/sampling`
- Arquivo: `dashboard/src/app/admin/sampling/page.tsx`
- Endpoints:
  - `GET /v1/admin/sampling-rules`
  - `POST /v1/admin/sampling-rules`
  - `PATCH /v1/admin/sampling-rules/{id}`
  - `DELETE /v1/admin/sampling-rules/{id}`
- Tipos suportados: `page_view`, `action`, `http_request`, `error`,
  `custom` (ou vazio = todos)
- Papel mínimo: **admin**

## Como usar

1. Abra **Sampling adaptativo**. A tabela lista escopo, taxa, bypass
   de erros, status, nota e data.
2. Clique **Nova regra** e preencha:
   - **App** — dropdown de apps ou "— todos —".
   - **Tipo** — dropdown de eventos ou "— todos —".
   - **Taxa de amostragem** — slider 0% a 100%, passo 5%. `0.2` =
     mantém 20% dos eventos que casam.
   - **Manter sempre erros** — checkbox marcado por default. Passa
     por bypass da taxa quando `HasError` ou `http_status ≥ 500`.
   - **Ativa** — desmarque para pausar sem excluir.
   - **Nota (opcional)** — contexto para outros admins.
3. **editar** reabre o modal com os valores atuais. **remover**
   exclui (com confirmação).

## O que você vê

- Cabeçalho com texto:  
  "Descarta uma fração dos eventos antes de gravar no ClickHouse.
  Precedência: regra mais específica ganha. Erros passam por bypass
  (`keepErrors`) para não perder sinais críticos ao amortizar volume."
- Tabela: **Escopo** (`app · eventType`, ambos podem ser `*`),
  **Taxa** (percentual, texto amarelo se `< 50%`), **Bypass erros**
  (`● sim` / `○ não`), **Status**, **Nota** (truncada), **Atualizada**,
  **Ações**.
- Estado vazio: "Nenhuma regra. Sem sampling, tudo é gravado no
  ClickHouse."

## Como demonstrar

> "É a válvula de custo do Ndovu. Em promoção, `page_view` do portal
> vai a 100k eventos/hora — configuro `10%` só nesse par e mantenho
> `100%` de erros. Custo cai 90%, sinal crítico intacto, sem tocar
> em uma linha de SDK."

Roteiro de 60s:
1. Aponte para uma regra existente: `portal-cliente · page_view` a
   `10%`, bypass de erro **sim**.
2. Explique precedência: regra mais específica (`app + type`) vence
   sobre a wildcard (`* · *`).
3. Crie **Nova regra** `* · custom` a `50%` com nota "amortiza
   eventos customizados que não precisamos de 100%".
4. Ative e rode o seed: `node tools/seed/seed.mjs --custom 100`.
5. Consulte no Explorer — cerca de 50 gravados. Injete um evento
   custom marcado como erro → todos passam (bypass).

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link.
- Admin só vê/cria regras para apps do próprio tenant —
  `tenantScope` filtra o CRUD.
- Regras `app = ""` (wildcard) só afetam apps do próprio tenant do
  admin que criou.
- `is_super` gerencia regras cross-tenant.

## Dependências

- O writer aplica as regras antes de gravar no ClickHouse — sem
  writer, sem sampling. A regra é consultada por evento; cache
  in-memory refresca em poucos segundos após PATCH/POST/DELETE.
- Métricas em tempo real e Explorer refletem o **efetivo** gravado —
  se você amostrou 10%, o Overview mostra 10% dos eventos.

## Perguntas frequentes

- **"Se eu criar regra `* · *` a 0%, apago tudo?"**  
  Quase — erros com `keepErrors=true` ainda entram. Sem esse bypass,
  sim, apaga tudo. Cuidado.
- **"Duas regras casam — qual ganha?"**  
  A mais específica: `app + type` > `app + *` > `* + type` > `* + *`.
  Empate entre `app + *` e `* + type` favorece `app + *`.
- **"Sampling atrapalha p95/p99 de latência?"**  
  Sim, se você amortizar `http_request` demais. Mantenha esse tipo em
  `100%` ou perto se latência é crítica.
- **"Consigo saber quanto amostrei?"**  
  Não há métrica dedicada na UI. Compare a taxa configurada com o
  volume observado no Overview após ativar.

## Referências

- Página: `dashboard/src/app/admin/sampling/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listSamplingRules`,
  `createSamplingRule`, `updateSamplingRule`, `deleteSamplingRule`
- Aplicador backend: `backend/internal/usecase/sampling` +
  `backend/internal/adapter/ingest`
- Docs relacionadas: [admin-alerts.md](admin-alerts.md),
  [visao-geral.md](visao-geral.md)
