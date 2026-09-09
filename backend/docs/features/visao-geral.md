# Visão geral (overview) — `/`

## Para que serve

A tela de aterrissagem do dashboard. Responde em 5 segundos: **"como
está o frontend agora?"** Traz os KPIs macro (eventos, sessões, usuários
únicos, erros, duração média) e uma série temporal para ver picos,
quedas e regressões sem precisar filtrar nada.

É a tela que você deixa aberta em um monitor de operação.

## Onde fica

- Rota: `/`
- Arquivo: `dashboard/src/app/page.tsx`
- Endpoint: `GET /v1/stats/overview?from=&to=&app=`
- Refresh automático: a cada 30s
- Papel mínimo: qualquer papel autenticado (admin, editor, viewer)

## Como usar

1. Abra o dashboard — a visão geral já carrega com janela **24h** e
   **todos os apps**.
2. Ajuste a janela no dropdown à direita (**5 min**, **1h**, **24h**,
   **7d**, **30d**) para investigar picos ou olhar tendência.
3. Filtre por app quando precisar isolar um portal específico. O filtro
   respeita o tenant scope: você só vê os apps aos quais tem acesso.
4. Clique **Atualizar** para forçar refresh imediato (o polling de 30s
   pega mudanças recentes sozinho).

## O que você vê

- **5 tiles no topo**
  - **Eventos** — total captado na janela.
  - **Sessões** — sessões únicas.
  - **Usuários** — `userId` distintos.
  - **Erros** — total + taxa (% dos eventos). Fica em vermelho quando > 0.
  - **Duração média** — tempo médio dos eventos com `durationMs` (ex.:
    HTTP requests, ações medidas).
- **Série temporal** dos eventos, com bucket automático conforme a
  janela (5min → 30d).
- **Top rotas** (média + p95 de duração) — quais URLs estão mais lentas.
- **Top erros** — códigos/mensagens mais frequentes na janela.

## Como demonstrar

> "Essa é a tela que fica no telão. Vejo em uma piscada quantos
> usuários estão ativos agora, se apareceu erro, e se alguma rota
> subiu de latência. Antes de qualquer dashboard de BI, o time de
> plataforma abre isso pra saber se hoje é dia calmo ou dia de fogo."

Roteiro de 60s:
1. Aponte os 5 tiles: "eventos, sessões, usuários — em tempo real".
2. Passe o mouse na série temporal e mostre a granularidade se
   ajustando conforme a janela.
3. Mude a janela pra **7d** e volte pra **24h** — ilustra elasticidade.
4. Filtre por um app específico — reforça multi-tenant.
5. Fale que ela **auto-atualiza a cada 30s** sem F5.

## Papéis (RBAC)

Todos os papéis autenticados enxergam a tela. O tenant scope garante
que **admin de Company A** só vê métricas dos apps de A — não há vazamento.

## Dependências

Precisa de **dados ingeridos**. Se a janela está vazia, aponte o SDK
para o app ou rode `node tools/seed/seed.mjs --sessions 40` no dev.

## Perguntas frequentes

- **"Por que o total de usuários é menor que sessões?"**  
  Um mesmo `userId` pode ter várias sessões (login em dispositivos
  diferentes, expiração de session).
- **"Por que a duração média inclui page_view?"**  
  Não inclui — só eventos com `durationMs` preenchido (HTTP requests
  e ações que passaram `durationMs`).
- **"Posso salvar minha combinação favorita de app + janela?"**  
  Ainda não nessa tela; use [saved views](saved-views.md) no
  Explorador de traces se quiser voltar rápido a um recorte.

## Referências

- Página: `dashboard/src/app/page.tsx`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` →
  `GetStatsOverview`
- Query ClickHouse: `backend/internal/adapter/clickhouse/queries.go`
- Docs relacionadas: [ARQUITETURA.md §5](../ARQUITETURA.md),
  [FUNCIONAL.md](../FUNCIONAL.md).
