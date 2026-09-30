# Performance / Web Vitals (`/performance`)

## Para que serve

Latência percebida pelo usuário real, não sintética. As **Core Web
Vitals** do Google (LCP, INP, FID, CLS) somadas a TTFB e FCP dizem se
seu SPA carrega rápido, responde rápido e não "pula" na tela. Esta
página mostra **p75 e p95 por rota**, com faixa de rating good /
needs-improvement / poor — a mesma classificação que o Chrome usa no
Lighthouse.

## Onde fica

- Rota: `/performance`
- Arquivo: `dashboard/src/app/performance/page.tsx`
- Endpoint: `GET /v1/stats/vitals`
- Refresh automático: a cada 60s
- Papel mínimo: qualquer papel autenticado

## Como usar

1. Escolha **App** e **Período** no topo — LCP pode ter poucas amostras
   em janelas curtas, então prefira 24h ou 7d.
2. A página gera uma seção por métrica (LCP, INP, FID, CLS, TTFB, FCP),
   cada uma com sua descrição curta e tabela por rota.
3. Leia p75 primeiro (o valor colorido): verde = bom, amarelo = precisa
   melhorar, vermelho = ruim. p95 mostra a cauda.
4. A coluna **good/poor** mostra a contagem absoluta — útil para saber
   se o p75 vermelho vem de 3 usuários ou 3 mil.
5. Clique **Atualizar** para forçar refresh (o polling de 60s pega
   novidades sozinho).

## O que você vê

- **Título** "Performance (Web Vitals)" + subtítulo "Métricas de
  experiência real de usuário — p75 e p95 por rota".
- Uma **seção por vital** com:
  - Nome (LCP, INP, FID, CLS, TTFB, FCP) e explicação curta.
  - Tabela: **Rota**, **amostras**, **p75** (colorido), **p95**,
    **good/poor**.
- Thresholds embutidos (alinhados com o SDK):
  - LCP 2500/4000 ms
  - INP 200/500 ms
  - FID 100/300 ms
  - CLS 0.1/0.25
  - TTFB 800/1800 ms
  - FCP 1800/3000 ms
- Vazio: "Sem Web Vitals coletados nesta janela. Confirme se o SDK
  está com `captureWebVitals: true`."

## Como demonstrar

> "Enquanto todo mundo mede performance com Lighthouse sintético, aqui
> é a experiência real do seu usuário — o que o navegador dele mediu.
> Você vê quais rotas passam, quais precisam de trabalho, e vê a cauda
> p95 para não se enganar com média."

Roteiro de 60s:
1. Abra `/performance` — mostre as 6 seções.
2. Aponte um LCP p75 amarelo em uma rota de listagem pesada.
3. Compare com o p95 — se a diferença for grande, cauda longa.
4. Filtre por **App** para isolar um portal.
5. Explique que a classificação segue a régua oficial do Google (Web
   Vitals), então falar disso com o time de front é falar a mesma
   língua.

## Papéis (RBAC)

Todos os papéis autenticados enxergam a tela. Tenant scope filtra
automaticamente por apps da company.

## Dependências

- SDK web com `captureWebVitals: true` (habilita a lib `web-vitals`
  no browser e envia eventos `web_vital_*` para o ingest).
- Como CLS e INP só chegam ao final da navegação/interação, janelas
  muito curtas (5 min) podem não ter amostras — prefira 24h.
- Backend usa ClickHouse para calcular percentis por rota.

## Perguntas frequentes

- **"Por que meu FID está vazio?"**
  Chrome descontinuou FID em favor de INP. Se todos seus usuários usam
  navegadores recentes, verá INP populado e FID quase zero — é
  esperado.
- **"O p75 mudou entre refreshes."**
  Como a janela é rolante, cada amostra nova pode empurrar o
  percentil. Em 60s a página busca de novo.
- **"CLS aparece com decimal — como interpreto?"**
  CLS é adimensional (score de deslocamento). ≤ 0.1 é bom; > 0.25 é
  ruim. A UI formata com 3 casas.

## Referências

- Página: `dashboard/src/app/performance/page.tsx`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `GetVitals`
- SDK: `sdk/web/src/vitals.ts` (captura via `web-vitals`)
- Docs relacionadas: [visao-geral.md](visao-geral.md),
  [releases.md](releases.md) (comparar vitals entre versões).
