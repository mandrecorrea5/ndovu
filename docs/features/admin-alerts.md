# Alertas (`/admin/alerts`)

## Para que serve

Regras de notificação simples e determinísticas: **"se X erros do
código Y no app Z em N segundos, dispara para tal URL"**. Cobre o
ponto crítico ("acabou de aparecer um `HTTP_500` no portal") sem
depender de linha de base histórica — para isso existe
[`/admin/anomalies`](admin-anomalies.md). O avaliador roda no writer
a cada minuto (configurável por `NDOVU_ALERTS_INTERVAL_SECONDS`) e
respeita **janela de silêncio** para não spamar durante incidente.

## Onde fica

- Rota: `/admin/alerts`
- Arquivo: `dashboard/src/app/admin/alerts/page.tsx`
- Endpoints:
  - `GET /v1/admin/alerts`
  - `POST /v1/admin/alerts`
  - `DELETE /v1/admin/alerts/{id}`
- Canais suportados: **Slack (webhook)** e **Webhook genérico**
- Papel mínimo: **admin**

## Como usar

1. Abra **Alertas**. A lista mostra as regras com escopo
   (app + errorCode), threshold/janela, canal e data.
2. Clique **Nova regra** e preencha:
   - **Nome** (ex.: "5xx no portal").
   - **App** — deixe vazio para "todos".
   - **Error code** (opcional, ex.: `HTTP_500`).
   - **Canal**: `Slack (webhook)` ou `Webhook genérico`.
   - **Threshold** (nº de erros) + **Janela (segundos)** — mínimo 30s.
   - **URL de destino** (obrigatória).
   - **Silêncio (segundos)** — default 900 (15 min) para evitar spam.
   - **ativa** — desmarque para pausar sem apagar.
3. **remover** exclui a regra (com confirmação).

## O que você vê

- Cabeçalho com o texto:  
  "Regras 'se X erros em Y segundos, notifica Z'. O avaliador roda no
  writer a cada minuto ... e respeita a janela de silêncio para não
  spamar."
- Tabela: **Nome**, **Escopo** (app + `· errorCode` em vermelho),
  **Threshold / janela** (`≥ N em Ys`), **Canal** (badge), **Criada**,
  **Ações**.
- Estado vazio: "Nenhuma regra ainda — comece por 'erros 5xx em
  qualquer app'."
- Modal em grade 2 colunas com os campos acima.

## Como demonstrar

> "É a rede de segurança de baixo custo: sem baseline nem estatística,
> só threshold explícito. Cria em 30 segundos, dispara no Slack em
> menos de 1 minuto e cala a boca por 15 minutos para não estourar o
> canal durante o incidente."

Roteiro de 60s:
1. Aponte para regra existente ("5xx no portal") — mostre threshold
   `≥ 5 em 300s`.
2. Clique **Nova regra**, configure erros do app `portal-cliente`,
   errorCode vazio, canal Slack, URL do webhook, silêncio 900s.
3. Salve. Simule erros via `curl` ou seed:
   `node tools/seed/seed.mjs --errors 10`.
4. Mostre a mensagem chegando no Slack.
5. Simule mais erros logo depois — nada dispara: o **silêncio** está
   em ação.

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link nem os endpoints.
- Admin de Company A só vê/cria regras para apps de A —
  `tenantScope` filtra.
- `is_super` vê e edita cross-tenant.

## Dependências

- Precisa de **apps cadastrados** para escopar por app (regra "todos"
  ignora esse filtro).
- Precisa de **eventos ingeridos** com o `errorCode` filtrado — o SDK
  frontend precisa emitir com esse campo preenchido, senão a regra
  nunca dispara.
- O avaliador roda no processo **writer**: se você desligar o writer,
  não dispara nada. Sem serviço separado.

## Perguntas frequentes

- **"E se eu quiser algo mais esperto que threshold fixo?"**  
  Use [Detecção de anomalia](admin-anomalies.md) — compara com
  baseline móvel por hora + dia da semana.
- **"O silêncio conta a partir do primeiro disparo ou do último
  evento?"**  
  Do último disparo bem-sucedido. Enquanto no silêncio, novas
  avaliações reconhecem o gatilho mas não notificam.
- **"Slack e webhook têm o mesmo payload?"**  
  Slack usa o formato `text` do webhook do Slack;
  webhook genérico posta JSON simples com nome da regra e métricas.
- **"Posso editar uma regra depois de criada?"**  
  Não pela UI — só **remover** e **criar de novo**. É intencional:
  regras são imutáveis por design para facilitar auditoria.

## Referências

- Página: `dashboard/src/app/admin/alerts/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listAlerts`, `createAlert`,
  `deleteAlert`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` +
  avaliador em `backend/internal/usecase/alerts`
- Docs relacionadas: [admin-anomalies.md](admin-anomalies.md),
  [admin-audit-log.md](admin-audit-log.md)
