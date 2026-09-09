# Releases (`/releases`)

## Para que serve

Toda vez que sobe uma versão, você quer saber duas coisas: **piorou
alguma coisa?** e **apareceu erro novo?**. Esta tela lista as
releases detectadas na janela e permite escolher **duas** (A e B)
para comparar delta de error rate, delta de duração média e listar
os fingerprints que só existem em B — regressões introduzidas no
deploy.

## Onde fica

- Rota: `/releases`
- Arquivo: `dashboard/src/app/releases/page.tsx`
- Endpoints:
  - `GET /v1/releases` (lista com métricas por release)
  - `GET /v1/releases/compare?releaseA=&releaseB=` (comparação)
- Papel mínimo: qualquer papel autenticado

## Como usar

1. Ajuste **App** e **Período** (default **7d**) e aguarde a lista.
2. A tabela mostra por release: **Eventos**, **Erros**, **Error rate**,
   **Sessões**, **Usuários**, **Duração média**, **Última atividade**.
3. Marque uma release como **A** e outra como **B** clicando nos
   botões da coluna **Ações** — o comparativo aparece embaixo assim
   que houver as duas seleções.
4. No painel de comparação, veja **Δ Error rate**, **Δ Duração
   média**, contagem de **Eventos B** vs A e **Novas issues em B**
   (fingerprints ausentes em A).
5. Clique **ver todas** na tabela de novas issues para saltar em
   `/issues` já filtrado por app e release.

## O que você vê

- **Tabela de releases** (min-width 900px). Error rate acima de 5%
  fica vermelho automaticamente.
- **4 tiles de comparação** com tom vermelho (danger) quando o delta é
  positivo (piorou).
- **Tabela "Novo erro em B"** limitada a 10 linhas + link "ver
  todas". Se B não trouxe nenhuma issue nova: "Nenhuma issue nova em
  B — release limpa em relação a A." (em verde).
- Vazio: "Nenhuma release detectada. Configure `release` no
  createNdovu do SDK ou envie via campo `release` do evento."

## Como demonstrar

> "Deploy é o momento de maior risco. Aqui, escolho a versão nova e a
> anterior, e em três segundos vejo se o error rate subiu, se ficou
> mais lento e — o mais importante — quais erros só existem agora,
> que não existiam antes. Se aparecerem 3 issues novas, eu já sei em
> qual PR mexer."

Roteiro de 60s:
1. Abra `/releases`, mostre 3-4 versões na tabela.
2. Marque a release atual como **B** e a anterior como **A**.
3. Aponte o Δ Error rate colorido de vermelho.
4. Role para as **novas issues** e destaque a mais frequente.
5. Clique **ver todas** — mostra o hand-off para `/issues`.

## Papéis (RBAC)

Todos os papéis autenticados enxergam a tela e o comparativo.
Nenhuma escrita — release é um agrupamento derivado dos eventos.

## Dependências

- **SDK deve enviar o campo `release`** em cada evento — normalmente
  configurado uma vez no `createNdovu({ release: '1.4.2' })`. Sem
  isso, a tela fica vazia.
- Backend agrupa por `(app, release)` e cruza fingerprints entre A e B
  para calcular `newIssues` (issues existentes em B e não em A).

## Perguntas frequentes

- **"O que conta como release?"**
  Qualquer string única. Convenção comum: versão semver
  (`1.4.2`), git sha curto (`abc1234`) ou tag de deploy
  (`prod-2026-08-24-1`). O importante é que seja **estável** — se
  todo deploy gera uma release nova, a lista polui rápido.
- **"Δ Error rate mostra + mas parece pouco. Isso preocupa?"**
  Depende do volume — 0,5% em 10M eventos são 50k erros. Cruze com
  o número de **Eventos B** para dimensionar.
- **"Posso comparar Web Vitals entre releases?"**
  Ainda não nesta tela. Use [performance.md](performance.md) filtrado
  por período — a comparação de vitals por release está no roadmap.

## Referências

- Página: `dashboard/src/app/releases/page.tsx`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `ListReleases`, `CompareReleases`
- SDK: campo `release` em `createNdovu` (documentado no README do SDK)
- Docs relacionadas: [issues.md](issues.md),
  [performance.md](performance.md).
